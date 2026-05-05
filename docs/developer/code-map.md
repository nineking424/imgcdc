# Code Map

소스 트리와 패키지 책임. 코드를 수정하기 전에 어디를 건드려야 하는지 빠르게 찾기 위한 지도.

```
imgcdc/
  cmd/imgcdc/main.go         flag 파싱, 로깅 초기화, app.Run 호출
  internal/
    app/app.go               root context, errgroup, channel(buf 256), shutdown timeout
    discovery/discovery.go   ReadDir + 패턴/날짜 필터, tailer spawn/retire
    tailer/tailer.go         파일 open + offset seek + bufio read + inode rotation
    parser/parser.go         keyword Contains + separator split + 절대경로 검증 (순수 함수)
    writer/writer.go         channel drain → catalog.WriteRecord
    catalog/
      catalog.go             Open, PRAGMA, DDL
      events.go              WriteRecord (단일 txn: INSERT + INSERT OR REPLACE)
      offsets.go             GetOffset, DeleteOffset, ErrNoOffset
    inode/inode_unix.go      syscall.Stat_t.Ino 추출 (build tag: unix)
  test/integration/
    integration_test.go      e2e: happy path + restart resume
  cmd/imgcdc/                main package
  Makefile                   build / build-linux / test / vet / fmt / clean
  go.mod                     Go 1.25.6, modernc.org/sqlite, golang.org/x/sync
  PRD.txt                    Spec (불변, 변경은 새 PRD 버전으로)
```

## cmd/imgcdc {#cmd}

진입점. `flag` 로 CLI 인자를 받아 `discovery.Config` 와 `dbPath` 를 만들어 `app.Run` 에 위임한다. signal 처리는 `signal.NotifyContext(ctx, SIGTERM, SIGINT)`.

수정 빈도 — flag 추가 시.

## internal/app {#app}

| 함수 / 상수 | 역할 |
|---|---|
| `ChannelBuffer = 256` | discovery → writer 채널 buffer |
| `Run(ctx, dbPath, dcfg, shutdownTimeout)` | catalog open → channel → errgroup(discovery, writer) → shutdown timeout |

discovery 가 종료될 때 `defer close(ch)` 로 채널을 닫고, writer 는 `for r := range w.in` 으로 자연 drain 후 종료한다.

## internal/discovery {#discovery}

| 심볼 | 역할 |
|---|---|
| `Config` | `LogDir`, `Pattern`, `Keyword`, `Separator`, `Grace`, `DiscoveryInterval`, `TailInterval`, `Now` (test inject) |
| `Discoverer.Run` | tick 마다 `reconcile`, ctx cancel 시 모든 active tailer cancel + WaitGroup |
| `reconcile` | desired = `scan()`, active 와 diff. 신규는 `runTailer` goroutine 스폰, 사라진 것은 cancel |
| `scan` | `os.ReadDir` + 패턴 매칭 + `parseDateFromName` + `inWindow` |
| `inWindow` | 오늘 또는 (어제 && `now - today_00 < grace`) |

`Now` 는 테스트 주입용 — 자정/grace 동작 검증에 사용.

## internal/tailer {#tailer}

| 심볼 | 역할 |
|---|---|
| `Config` | `Path`, `Keyword`, `Separator`, `Interval` |
| `Tailer.Run` | `open` 클로저로 파일 열고, ctx 취소 또는 read 에러까지 loop |
| `open` (클로저) | open + `GetOffset` + inode 일치 시 seek, 불일치 시 offset=0 |
| `rotated` (클로저) | `os.Stat` 로 현재 inode 와 비교 |

partial line 은 byte slice (`partial`) 로 다음 read 까지 보류한다. EOF 시 `time.After(cfg.Interval)` 만큼 대기.

## internal/parser {#parser}

순수 함수 한 개:

```go
Parse(line, keyword, separator string, now func() time.Time) (Event, error)
```

| 결과 | 의미 |
|---|---|
| `Event{Path, TSNs}` 반환 | 매칭 + 절대경로 검증 통과 |
| `ErrNoMatch` | 키워드 미포함 — tailer 에서 silent skip |
| `ErrMalformed` | 키워드 매칭이지만 separator split 실패 또는 비절대경로 — tailer 에서 warn |

## internal/writer {#writer}

| 심볼 | 역할 |
|---|---|
| `Writer.Run(ctx)` | `for r := range w.in { db.WriteRecord(ctx, r) }` |

채널이 닫힐 때까지 drain. 채널 close 책임은 호출 측 (현재 `app.Run` → discovery 가 `defer close`).

## internal/catalog {#catalog}

| 파일 | 역할 |
|---|---|
| `catalog.go` | `Open(ctx, path)` — DSN 으로 `journal_mode=WAL` + `busy_timeout=5000`, 그 후 PRAGMA 4종 + DDL. `SetMaxOpenConns(1)` |
| `events.go` | `WriteRecord` — `BEGIN; INSERT file_events; INSERT OR REPLACE tail_offsets; COMMIT` |
| `offsets.go` | `GetOffset` (없으면 `ErrNoOffset`), `DeleteOffset` |

`schemaVersion = 1` 은 `catalog.go` 상수. 변경 시 migration 절차 신규 작성 필요 (현재 미구현).

## internal/inode {#inode}

`Of(info os.FileInfo) uint64` — `syscall.Stat_t.Ino` 추출. build tag `unix` (linux + darwin). Windows 빌드는 미지원.

## test/integration {#integration}

`TestEndToEnd_HappyPath` — 라인 5건 append → catalog 5 row 검증.
`TestEndToEnd_ResumesAfterRestart` — 3건 처리 후 restart → 4건 추가 후 총 7 row + 순서 검증.

`waitForRows` helper 가 50ms 폴링으로 row count 가 도달할 때까지 대기.

## 패키지 의존 방향

```
cmd/imgcdc → internal/app → {discovery, writer, catalog}
                            discovery → tailer → {parser, catalog, inode}
                            writer → catalog
                            tailer → catalog (offset 조회)
```

순환 없음. `internal/catalog` 는 leaf 패키지.
