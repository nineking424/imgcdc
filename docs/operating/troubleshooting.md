# Troubleshooting

증상 → 원인 → 진단(단일 명령) → 조치 → 검증.

각 항목 안의 진단 단계는 `1` 회로 끝난다 (yes/no 신호). 더 깊은 조사가 필요한 경우 [runbook.md](runbook.md) 의 해당 절로 넘긴다.

## 1. 새 이벤트가 catalog 에 안 들어옴 {#1-no-events}

**Cause.** discovery 가 파일을 못 잡거나, tailer 가 못 열거나, 라인이 키워드 매칭에 실패하거나.

**Diagnose.** 마지막 commit 시점이 얼마나 오래됐는지:

```sql
SELECT (strftime('%s','now') - MAX(updated_ns)/1000000000) AS sec_since_last_commit
FROM tail_offsets;
```

`sec_since_last_commit` 이 평소 패턴(피크 시 수 초, 한산 시 분 단위)을 크게 넘으면 비정상.

**Act.**

| 결과 | 의미 | 다음 |
|---|---|---|
| `tail_offsets` 가 비어있음 | discovery 가 한 파일도 못 잡았다 | `--log-dir` / `--file-pattern` / `ls` 로 매칭 여부 확인 |
| 일부 파일 row 만 stale | 그 파일의 tailer 가 멈췄다 | [§3-stuck-tailer](#3-stuck-tailer) |
| 모든 row 가 stale | writer 가 막혔다 | [§4-db-locked](#4-db-locked) |

**Verify.** 새 라인을 ETL 로그에 직접 append (테스트가 가능한 환경) 후 1–2초 안에 `seq_id` 가 증가하는지.

## 2. 이벤트가 늦게 들어옴 {#2-stale-events}

**Cause.** `--tail-interval` 이 길거나, ETL 로그 자체가 늦게 쓰이거나, channel saturation.

**Diagnose.** 가장 최신 row 의 `event_ts_ns` 와 현재 wall clock 차이:

```sql
SELECT (strftime('%s','now') - MAX(event_ts_ns)/1000000000) AS lag_sec FROM file_events;
```

| `lag_sec` | 해석 |
|---|---|
| 0–5 | 정상 |
| 5–60 | tail interval 이 길거나 한산한 시간대 |
| 60+ | ETL 측 지연 또는 imgcdc 측 stall — [§3-stuck-tailer](#3-stuck-tailer) |

**Act.** ETL 로그를 직접 `tail -f` 해 새 라인이 실시간으로 떨어지는지 확인. 떨어지지 않으면 ETL 측 문제 — imgcdc 외부.

**Verify.** `--tail-interval=500ms` 로 단축 후 lag 가 줄면 폴링 주기가 원인.

## 3. 특정 파일의 tailer 가 멈춤 {#3-stuck-tailer}

**Cause.** 파일 권한 거부, 디스크 read 에러, 또는 EOF 후 inode 가 사라진 채로 ReadDir 이 동일 이름을 계속 보고하는 케이스.

**Diagnose.** 데몬 로그에서 해당 파일의 마지막 메시지:

```sh
grep -E "tailer exited|inode rotation|file=/var/log/etl/...log" /var/log/upstart/imgcdc.log | tail -20
```

**Act.**

| 로그 메시지 | 의미 | 조치 |
|---|---|---|
| `tailer exited with error err="open ...permission denied"` | 권한 문제 | imgcdc 데몬 user 의 read 권한 부여 |
| `tailer exited with error err="read: ..."` | 디스크 / FS 에러 | dmesg 확인, 호스트 레벨 점검 |
| 메시지가 없고 stale | reconcile 이 desired 에서 제외했거나 ctx cancel | discovery 의 spawn/retire 로그 확인 |

stuck 이라고 판단되면 데몬 재기동: [runbook.md#1-graceful-restart](runbook.md#1-graceful-restart). 재기동은 마지막 offset 부터 안전하게 재개한다.

**Verify.** 재기동 후 해당 파일의 `tail_offsets.updated_ns` 가 갱신되는지.

## 4. SQLite "database is locked" {#4-db-locked}

**Cause.** writer 외 다른 writer connection 이 catalog 에 쓰려는 시도. 정상 운영에서는 발생하면 안 된다.

**Diagnose.** 누가 catalog.db 를 쓰는지:

```sh
lsof /var/lib/imgcdc/catalog.db
```

**Act.**

| 결과 | 조치 |
|---|---|
| imgcdc 만 보임 | `--db` 경로가 NFS 등 lock 미지원 FS 아닌지 확인 — 로컬 디스크 권장 |
| consumer 가 쓰기 모드로 열고 있음 | consumer 코드를 read-only 로 수정 — [../consumer/guide.md](../consumer/guide.md) |
| 두 imgcdc 인스턴스 | 즉시 한 쪽 종료. supervisor 중복 기동 점검 |

`busy_timeout=5000` (5초) 가 DSN 으로 걸려있어 일시적 경합은 자동 재시도된다. 그래도 실패한다면 진짜 lock 충돌이다.

**Verify.** `lsof` 결과가 imgcdc + read-only consumer 만 남는지.

## 5. 채널 saturation {#5-channel-saturation}

**Cause.** writer 가 멈춰서 채널 buffer 256 이 가득 참. 0.06 evt/s 부하에서는 사실상 불가.

**Diagnose.** writer 의 마지막 commit 시각:

```sql
SELECT datetime(MAX(updated_ns)/1000000000, 'unixepoch') FROM tail_offsets;
```

이 값이 정체되어 있고 ETL 로그는 계속 쓰이고 있다면 writer stall.

**Act.** SQLite write 실패가 누적된 케이스 — [§4-db-locked](#4-db-locked) 또는 [runbook.md#5-disk-full](runbook.md#5-disk-full) 로.

**Verify.** writer 가 풀리면 backpressure 가 해소되며 다음 polling 사이클에 lag 가 회수된다 — 데이터 손실은 없다.

## 6. Malformed line warning {#6-malformed-lines}

**Cause.** 키워드는 매칭됐는데 separator 분리 후 마지막 토큰이 절대경로가 아니다.

**Diagnose.** 데몬 로그 grep:

```sh
grep "malformed line" /var/log/upstart/imgcdc.log | tail -20
```

**Act.** 데이터 손실 없이 skip 처리되므로 즉시 조치 불요. 다만 ETL 측 포맷이 바뀌었다면 imgcdc 의 `--keyword` / `--path-separator` 조정 또는 ETL 측 로그 포맷 정정.

**Verify.** 정정 후 같은 라인이 더 이상 warn 으로 안 뜨는지. 누락된 이벤트가 있다면 [runbook.md#6-removed-file](runbook.md#6-removed-file) 의 `tail_offsets` 조작으로 재처리 가능 (해당 파일을 offset 0 으로 reset).

## 알려진 한계

- 메트릭 / 알림 인프라 미연동. 진단은 모두 stdout/stderr 로그 + ad-hoc SQL 쿼리에 의존한다.
- malformed line 카운터 없음 — `grep | wc -l` 로 추정해야 한다.
