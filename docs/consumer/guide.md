# Consumer Guide

`catalog.db` 를 read-only 로 따라가며 새 info 파일을 처리하는 consumer 프로세스의 작성법.

이벤트 보증 (ordering, at-least-once) 의 자세한 설명은 [../concepts/catalog-schema.md](../concepts/catalog-schema.md) 를 본다. 이 문서는 그 보증 위에서 코드를 어떻게 짜는지에 집중한다.

## Read-only 접근 {#access}

| 항목 | 권장 |
|---|---|
| open mode | read-only (`mode=ro` DSN 또는 동등 옵션) |
| 동시 접근 | 다수 consumer 동시 가능 — SQLite WAL 이 reader 를 차단하지 않음 |
| 트랜잭션 | 짧게 — 긴 read txn 은 WAL checkpoint 를 막아 디스크 사용량을 키운다 |

쓰기 시도는 절대 금지. imgcdc 의 단일 writer 원칙이 깨진다.

Go 예시:

```go
db, err := sql.Open("sqlite", "/var/lib/imgcdc/catalog.db?mode=ro&_pragma=busy_timeout(5000)")
```

## Consumption query {#query}

```sql
SELECT seq_id, event_type, path, event_ts_ns
FROM file_events
WHERE seq_id > :last_seen_seq_id
ORDER BY seq_id
LIMIT :batch_size;
```

| 파라미터 | 권장 | 비고 |
|---|---|---|
| `:last_seen_seq_id` | consumer 가 자체 저장한 cursor | 첫 실행은 `0` |
| `:batch_size` | 100 ~ 1,000 | 부하 0.06 evt/s 라 작아도 무방 |

## Cursor 관리 {#cursor}

각 consumer 는 자신의 `last_seen_seq_id` 를 *자체* 영속화한다 — imgcdc 는 consumer 별 상태를 가지지 않는다.

권장 패턴:

1. 배치 N 건 SELECT.
2. 각 row 에 대해 처리 (idempotent — 아래 참고).
3. 처리 완료 후 가장 큰 `seq_id` 를 cursor 로 commit.
4. cursor commit 이 실패하면 다음 사이클에 같은 batch 를 다시 받는다 (at-least-once).

cursor 저장소 후보:

| 저장소 | 적합성 |
|---|---|
| 별도 SQLite 파일 | 가장 단순. consumer 한 개당 한 파일 |
| 후속 처리 결과 DB 의 별도 테이블 | 처리 결과 commit 과 같은 txn 으로 묶을 수 있어 atomic |
| 파일 (`/var/lib/<consumer>/cursor`) | 단순하지만 atomic 갱신은 직접 처리 (rename trick) |

cursor 와 처리 결과를 같은 txn 으로 묶을 수 있다면 그 패턴이 가장 안전하다 — partial 처리가 원천 봉쇄된다.

## Idempotency {#idempotency}

at-least-once 라서 같은 row 를 두 번 처리할 수 있다. 게다가 외부 원인(ETL 로그가 같은 라인을 두 번 쓰는 케이스)으로 동일 path 가 다른 `seq_id` 로 두 번 들어올 수도 있다.

| 중복 종류 | 어디서 | 회피법 |
|---|---|---|
| 같은 `seq_id` 재처리 | consumer cursor commit 실패 후 재시작 | `seq_id` 기반 dedup 또는 처리 결과 자체가 idempotent |
| 같은 `path` 가 다른 `seq_id` | ETL 로그 재기록 | `path` 기반 dedup (예: `processed_paths(path PRIMARY KEY)`) |

권장: cursor 를 신뢰하면서, 처리 결과가 path 기준 unique 가 되게 짠다.

## Ordering {#ordering}

`seq_id` 단조 증가 + 단일 writer 로 단일 DB 안에서 total order 가 보장된다. 단:

- `event_ts_ns` 는 imgcdc 가 라인을 read 한 wall clock 이라 *완전히 단조*는 아니다 (clock skew 가능). 시간 정렬이 필요하면 `seq_id` 기준으로 한다.
- `path` 가 같은 row 가 여러 `seq_id` 에 걸쳐 등장할 수 있다 (재처리 / late-arriving).

## ENOENT 대응 {#enoent}

consumer 가 `path` 를 처리하려는 시점에 파일이 이미 사라졌을 수 있다 — info 파일은 short-lived 일 수 있다.

권장 처리:

| 상황 | 처리 |
|---|---|
| 정상 처리 가능 | 처리 후 cursor advance |
| `ENOENT` | warn 로깅 + cursor advance (skip) |
| 그 외 read 에러 | retry with backoff. 일정 회수 초과 시 dead-letter 또는 escalate |

cursor 를 advance 하지 않고 영구 retry 하면 그 path 에서 진행이 멈춘다. 정책상 skip 이 맞으면 명시적으로 advance 한다.

## 폴링 주기 {#polling}

| 요건 | 권장 주기 |
|---|---|
| 분 단위 latency 로 충분 | 30s ~ 60s |
| 초 단위 latency 필요 | 1s ~ 5s |
| 부하 우려 | longer is fine — 이벤트 부하 자체가 0.06 evt/s 라 catalog 에 늘 새 row 가 있는 게 아니다 |

`SELECT` 가 비어있으면 sleep 후 재시도. 별도 알림 채널은 없다 — pure pull.

## 끝까지 따라잡기 (catch-up) {#catchup}

처음 기동하거나 오래 멈춰있던 consumer 는 한참 뒤처진다. `LIMIT :batch_size` 로 끊어 읽으면 된다 — 1년치 240 MB / 1.8M row 도 batch=1000 이면 1,800 round-trip 으로 끝난다.

catch-up 중에는 `event_ts_ns` 가 아니라 *현재 wall clock* 으로 처리되므로, 시간 의존 처리(예: TTL 비교)는 `event_ts_ns` 를 사용한다.

## 예시 (Go) {#example}

```go
package main

import (
    "context"
    "database/sql"
    "fmt"
    "time"

    _ "modernc.org/sqlite"
)

func main() {
    db, err := sql.Open("sqlite",
        "/var/lib/imgcdc/catalog.db?mode=ro&_pragma=busy_timeout(5000)")
    if err != nil { panic(err) }
    defer db.Close()

    var cursor int64 = loadCursor()  // 자체 저장소에서 복원

    for {
        rows, err := db.QueryContext(context.Background(), `
            SELECT seq_id, path, event_ts_ns
            FROM file_events
            WHERE seq_id > ?
            ORDER BY seq_id
            LIMIT 500`, cursor)
        if err != nil { /* log + sleep + continue */ }

        var lastSeq int64 = cursor
        for rows.Next() {
            var seq, ts int64
            var path string
            rows.Scan(&seq, &path, &ts)
            if err := process(path, ts); err != nil {
                // ENOENT → skip (log + advance), 그 외는 retry
            }
            lastSeq = seq
        }
        rows.Close()

        if lastSeq > cursor {
            saveCursor(lastSeq)
            cursor = lastSeq
        }
        time.Sleep(30 * time.Second)
    }
    _ = fmt.Sprintf
}
```

`process` 와 `saveCursor` 를 같은 트랜잭션으로 묶을 수 있다면 (consumer 측 DB 와 함께) at-least-once 의 중복 처리도 자연스럽게 흡수된다.
