# Runbook

운영 중 자주 마주치는 상황별 표준 절차.

각 항목은 *언제 쓰는가 → 절차 → 검증* 순으로 본다. 증상이 모호하면 먼저 [troubleshooting.md](troubleshooting.md) 로 분류한다.

## 1. Graceful restart {#1-graceful-restart}

**언제.** 설정 변경(grace, log-level 등), 바이너리 업그레이드, 호스트 재부팅 직전.

**절차.**

```sh
stop imgcdc       # SIGTERM → discovery cancel → tailer drain → writer commit
start imgcdc
```

기본 `--shutdown-timeout=5s` 안에 깔끔히 종료된다 — 부하가 0.06 evt/s 라 채널 drain 이 거의 즉시 끝난다.

**검증.** 마지막 처리 위치에서 정확히 재개됐는지:

```sql
SELECT file, offset, datetime(updated_ns/1000000000, 'unixepoch') AS last_commit
FROM tail_offsets;
```

`last_commit` 이 stop 시각과 같거나 미세하게 앞서면 정상.

## 2. Crash recovery {#2-crash-recovery}

**언제.** 데몬이 SIGKILL 또는 panic 으로 비정상 종료된 경우. supervisor 가 자동 재기동했을 가능성이 높다.

**절차.** 별도 작업 없음. 재기동만 하면 된다.

```sh
status imgcdc
start imgcdc      # 이미 running 이면 skip
```

**왜 안전한가.** writer 의 단일 txn 이 `INSERT file_events` + `INSERT OR REPLACE tail_offsets` 를 같이 commit 한다. 따라서:

- commit 전 crash → row 도 offset 도 없음 → 재기동 시 같은 라인 다시 읽음 (no-loss)
- commit 후 crash → row 와 offset 모두 보존 → 재기동 시 다음 라인부터 (no-dup)

자세한 보증은 [../concepts/catalog-schema.md#delivery](../concepts/catalog-schema.md#delivery).

**검증.** `seq_id` 의 단조성 + 마지막 라인의 path 가 ETL 로그의 마지막 매칭 라인과 일치:

```sql
SELECT MAX(seq_id), COUNT(*) FROM file_events;
```

## 3. Inode rotation {#3-inode-rotation}

**언제.** 외부 스크립트가 로그 파일을 mv + 새 파일 생성한 경우. 표준 운영은 일자별 새 파일이라 회전이 일어나지 않지만 안전망으로 처리된다.

**절차.** 자동 처리. tailer 가 EOF 도달 시 `os.Stat` 으로 inode 비교 → 다르면 reopen + offset 0 (`internal/tailer/tailer.go` 의 `rotated()` / `open()`).

```
INFO inode rotation detected file=/var/log/etl/etl_defectimg_work01_2026_05_06.log
```

**검증.** 회전 직후 이벤트가 누락 없이 잡혔는지:

```sql
SELECT seq_id, path, datetime(event_ts_ns/1000000000, 'unixepoch')
FROM file_events
ORDER BY seq_id DESC
LIMIT 20;
```

회전 시점 전후로 row 가 연속해야 한다. 회전 직전 라인이 commit 되지 않았다면 새 파일에서 그 라인이 다시 등장하지는 않는다 — ETL 로그가 회전 정책상 *옮겨졌을 뿐 잘렸을 리 없다* 는 가정에 의존한다.

## 4. 일자 전환 / Grace 만료 {#4-log-rotation}

**언제.** 자정 이후 어제 날짜 파일을 언제 retire 할지 확인하고 싶은 경우.

**규칙.** discovery 의 `inWindow` 함수:

| 파일 날짜 | 현재 시각 | 활성 |
|---|---|---|
| 오늘 | 항상 | 예 |
| 어제 | `now - today_00:00 < grace` | 예 |
| 그 외 | — | 아니오 |

기본 `grace=90m` 이면 어제 파일은 **오늘 01:30 까지** 유지된다.

**절차.** retire 는 자동. 로그에 다음이 보이면 정상:

```
INFO retiring tailer file=/var/log/etl/etl_defectimg_work01_2026_05_05.log
```

**검증.** 활성 tailer 목록 — 직접 노출은 없으므로 stdout/stderr 의 최근 spawn/retire 로그 + `tail_offsets.updated_ns` 가 갱신 중인 파일로 추정한다.

## 5. 디스크 풀 {#5-disk-full}

**언제.** `/var/lib/imgcdc/` 의 파티션이 가득 찬 경우. SQLite write 가 실패하면 writer goroutine 이 error 를 반환해 데몬 전체가 비정상 종료된다.

**증상.** stderr 에:

```
ERROR imgcdc exited err="writer: insert file_events: database or disk is full"
```

**절차.**

1. 디스크 사용량 확인 — `df -h /var/lib/imgcdc`.
2. 가능한 회수: WAL 파일 (`catalog.db-wal`) 가 비대한지 확인. `wal_autocheckpoint=10000` 이지만 reader 가 잡혀있으면 checkpoint 가 지연된다.
3. 공간 확보 후 supervisor 가 재기동. crash recovery 가 알아서 처리한다 ([§2](#2-crash-recovery)).

**검증.** 재기동 후 새 row 가 들어오는지:

```sql
SELECT MAX(seq_id), datetime(MAX(event_ts_ns)/1000000000, 'unixepoch') FROM file_events;
```

## 6. 파일이 사라짐 {#6-removed-file}

**언제.** 외부에서 `/var/log/etl/*.log` 가 삭제된 경우.

**절차.** discovery 가 desired set 에서 제거 → tailer ctx cancel → 자동 retire.

`tail_offsets` row 는 자동 삭제되지 *않는다* — 같은 파일명이 다시 등장하면 inode 비교에서 mismatch 가 나 offset 0 부터 읽는다 (의도된 동작). 영구히 사라진 경우 row 를 정리하고 싶다면:

```sql
DELETE FROM tail_offsets WHERE file = '/var/log/etl/etl_defectimg_work01_2026_05_05.log';
```

**검증.** `tail_offsets` 의 row 수 ≈ 활성 + 어제 파일 수. 한참 오래된 파일이 다수 남아있다면 위 쿼리로 정리.

## 알려진 한계

- retire 트리거는 *디스커버리 시점에 desired set 에 없을 때* 한 번 일어난다. PRD §7.1 이 언급한 "grace 경과 + 60s EOF stable" 추가 조건은 현 구현에 없다 — grace 시점에 즉시 cancel.
- Schema migration 스텝 없음. `user_version=1` 그대로. 향후 schema 변경 시 별도 절차 필요.
