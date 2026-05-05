# Catalog Schema

`catalog.db` 의 테이블, PRAGMA, ordering / delivery 보증.

스키마 적용 코드는 `internal/catalog/catalog.go` 의 `ddl` 상수와 `pragmas` 슬라이스. 변경 시 두 곳을 함께 본다.

## file_events {#file-events}

| 컬럼 | 타입 | 설명 |
|---|---|---|
| `seq_id` | `INTEGER PRIMARY KEY AUTOINCREMENT` | 단조 증가, consumer 의 cursor |
| `event_type` | `INTEGER NOT NULL` | 현재 항상 `1` (UPSERT) — 향후 호환용 |
| `path` | `TEXT NOT NULL` | 절대 real path |
| `event_ts_ns` | `INTEGER NOT NULL` | 라인 수신 시점의 wall clock (`time.Now().UnixNano()`) |

`CREATE INDEX idx_file_events_path ON file_events(path)` — path 기준 dedup / 검색용.

이벤트 타임스탬프는 라인 안의 timestamp 가 아니라 imgcdc 가 라인을 read 한 시점이다. timezone / 형식 의존성 회피 목적 (`PRD.txt` D-015).

## tail_offsets {#tail-offsets}

| 컬럼 | 타입 | 설명 |
|---|---|---|
| `file` | `TEXT PRIMARY KEY` | 로그 파일 절대경로 |
| `offset` | `INTEGER NOT NULL` | 다음에 읽기 시작할 byte |
| `inode` | `INTEGER NOT NULL` | rotation 검출용 |
| `updated_ns` | `INTEGER NOT NULL` | 이 row 가 commit 된 wall clock |

재기동 시 tailer 는 이 row 의 `inode` 가 현재 파일 inode 와 같을 때만 `offset` 으로 seek 한다. inode 가 달라졌으면 회전된 것으로 보고 offset 0 부터 다시 읽는다 — `internal/tailer/tailer.go` 의 `open` 클로저.

## PRAGMA {#pragmas}

| PRAGMA | 값 | 적용 위치 |
|---|---|---|
| `journal_mode` | `WAL` | DSN: `?_pragma=journal_mode(WAL)` |
| `busy_timeout` | `5000` | DSN: `?_pragma=busy_timeout(5000)` |
| `synchronous` | `NORMAL` | `Open` 후 `ExecContext` |
| `wal_autocheckpoint` | `10000` | 동상 |
| `foreign_keys` | `OFF` | 동상 |
| `user_version` | `1` | 동상 |

`journal_mode=WAL` 만 DSN 으로 적용한다 — 별도 connection 이 non-WAL 로 먼저 attach 한 뒤 `PRAGMA journal_mode=WAL` 가 EXCLUSIVE lock 을 못 잡고 hang 하는 race 를 회피한다 (commit `b1c7d2a`).

추가로 connection pool 은 `SetMaxOpenConns(1)` — 단일 writer 원칙을 SQL 레이어에서 강제한다.

## Ordering 보증 {#ordering}

- `seq_id` 는 SQLite AUTOINCREMENT 의 정의에 따라 단일 DB 안에서 단조 증가 + 재사용 없음.
- writer 는 단일 goroutine + 단일 connection — txn commit 순서가 곧 `seq_id` 순서.
- 동일 라인을 여러 row 가 가질 수 있다 (재처리). path 가 같다고 같은 이벤트는 아니다 — `seq_id` 가 식별자.

## Delivery 보증 {#delivery}

at-least-once.

| 시나리오 | 결과 |
|---|---|
| 정상 commit | row 가 보이면 `tail_offsets.offset` 도 같이 commit 됨 |
| txn 도중 crash | row 도 offset 도 보이지 않음 → 재기동 시 같은 라인을 다시 read |
| commit 직후 crash | row 와 offset 모두 보존 → 재기동 시 다음 라인부터 |

같은 라인이 두 row 가 되는 경우는 없다 — `INSERT file_events` 와 `INSERT OR REPLACE tail_offsets` 가 같은 txn 이라 부분 commit 이 불가능하다.

대신 외부 원인의 중복은 가능하다:

- ETL 로그가 같은 라인을 두 번 쓴 경우
- 운영자가 `tail_offsets` 를 수동 변경한 경우

따라서 consumer 는 idempotent 처리 + path 기반 dedup 를 권장한다 — [consumer/guide.md#idempotency](../consumer/guide.md#idempotency) 를 본다.

## 사이징 {#sizing}

| 항목 | 값 |
|---|---|
| 평균 row 크기 | ~130 byte |
| 일 row 수 | ~5,000 |
| 일 증가량 | ~650 KB |
| 1년 누적 | ~240 MB |

MVP 는 cleanup 을 두지 않는다 (`PRD.txt` D-007). 1년 운용 후 retention 도입 여부를 재평가한다.

## 다루지 않는 것 {#non-goals}

- DELETE / MODIFY 이벤트 — 소스 로그에 정보 없음.
- 파일 size / mtime / inode 등 stat 결과 — catalog 의 1차 가치에 기여하지 않음. consumer 가 필요 시 직접 stat (`PRD.txt` D-006).
- schema migration — `user_version=1` 고정. 변경 시 migration step 추가 예정 (현재 미구현).
