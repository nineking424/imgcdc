# Architecture

imgcdc 가 ETL 로그 라인 한 줄을 catalog row 한 행으로 변환하기까지의 데이터 흐름과 책임 분리.

## 데이터 흐름

```
ETL workers (외부)
        │ append
        v
/var/log/etl/etl_defectimg_workNN_YYYY_MM_DD.log
        │
   ┌────┴──────────────────────────────────────────┐
   │ imgcdc daemon (single static ELF)             │
   │                                               │
   │  discovery (1 goroutine)                      │
   │   - cfg.DiscoveryInterval 마다 ReadDir        │
   │   - 패턴 매칭 + (today | yesterday<grace) 필터│
   │   - 신규 파일 → tailer goroutine spawn        │
   │   - 윈도우 이탈 → ctx cancel 로 retire        │
   │           │                                   │
   │           v                                   │
   │  per-file tailer (~50 goroutine)              │
   │   - bufio.Reader.ReadBytes('\n')              │
   │   - parser.Parse → keyword 매칭 + path 추출   │
   │   - EOF 시 cfg.TailInterval sleep             │
   │   - inode 변화 감지 시 reopen                  │
   │           │ chan catalog.Record (buf 256)     │
   │           v                                   │
   │  writer (1 goroutine)                         │
   │   - per-event txn:                            │
   │       INSERT file_events                      │
   │       INSERT OR REPLACE tail_offsets          │
   │       COMMIT                                  │
   │                                               │
   │  signal handler (SIGTERM/SIGINT)              │
   │   - errgroup ctx cancel                       │
   │   - shutdown timeout 후 강제 종료             │
   └───────────────────────────────────────────────┘
        │
        v
  /var/lib/imgcdc/catalog.db (SQLite WAL)
        │
        v
  consumer A, B, ..., N (별 프로세스, read-only)
```

## 컴포넌트 {#components}

| 컴포넌트 | 위치 | 책임 |
|---|---|---|
| signal / lifecycle | `cmd/imgcdc/main.go`, `internal/app` | flag 파싱, root context 구성, errgroup 합류, shutdown timeout |
| discovery | `internal/discovery` | 디렉토리 ReadDir, 패턴+날짜 윈도우 필터, tailer spawn/retire |
| tailer | `internal/tailer` | 단일 파일 open, offset seek, 라인 read, partial buffering, inode rotation 감지 |
| parser | `internal/parser` | 키워드 매칭 + separator 분리 + 절대경로 검증 (순수 함수) |
| writer | `internal/writer` | 채널 drain → catalog.WriteRecord 호출 |
| catalog | `internal/catalog` | SQLite open, 스키마/PRAGMA 적용, 단일 txn 안에서 INSERT + UPSERT |
| inode helper | `internal/inode` | `syscall.Stat_t` 에서 inode 추출 (build tag `unix`) |

## 라이프사이클 {#lifecycle}

1. `app.Run` 이 catalog 를 open 하고 `chan catalog.Record` (buf 256, `app.ChannelBuffer`) 를 만든다.
2. errgroup 에 두 goroutine 을 등록한다 — discovery 와 writer.
3. discovery 는 자신이 종료될 때 `defer close(ch)` 로 채널을 닫는다 — writer 는 `for r := range w.in` 으로 자연 종료한다.
4. discovery 는 매 tick 마다 `reconcile` 로 active map 과 desired set 을 비교 — 신규는 spawn, 사라진 건 ctx cancel.
5. tailer 는 EOF 시 `cfg.TailInterval` sleep 후 재시도. ctx cancel 시 즉시 종료.

## Shutdown {#shutdown}

| 단계 | 트리거 | 동작 |
|---|---|---|
| 1. 신호 수신 | `SIGTERM` 또는 `SIGINT` | `signal.NotifyContext` 가 root ctx 를 cancel |
| 2. discovery 종료 | ctx.Done | 모든 active tailer ctx cancel + WaitGroup 대기 후 채널 close |
| 3. writer 종료 | 입력 채널 close | 남은 record 모두 commit 후 nil 반환 |
| 4. 강제 종료 | `--shutdown-timeout` 초과 | `app.Run` 이 `errors.New("shutdown timeout exceeded")` 반환 |

기본 shutdown timeout 은 5s (`--shutdown-timeout` 으로 조정).

## 단일 writer 원칙

tailer 는 SQLite 핸들을 직접 호출하지 않고 채널로만 통신한다. 단일 writer 가 단일 SQLite 연결(`SetMaxOpenConns(1)`) 로 직렬 commit 하므로 lock 경합이 없다. 더 자세한 commit 원자성은 [catalog-schema.md#delivery](catalog-schema.md#delivery) 를 본다.

## 의도적으로 두지 않은 것 {#non-goals}

- ring buffer / lock-free MPSC — Go channel 로 충분하다.
- worker pool / batch insert — 0.06 evt/s 부하에서 무의미.
- DELETE / MODIFY 이벤트 처리 — 소스 부재.
- cold-start 디렉토리 스캔 — 새 출현만 추적, backfill 없음.
- inotify / fanotify — 운영 정책상 사용 불가.
