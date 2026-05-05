# Testing

테스트 구조와 실행 방법.

| 레이어 | 위치 | 내용 |
|---|---|---|
| Unit | `internal/*/...test.go` | 패키지별 순수 단위 테스트 |
| Integration | `test/integration/integration_test.go` | 실 SQLite + 실 파일로 e2e |

## 실행 {#commands}

| 명령 | 동작 |
|---|---|
| `make test` | 전체 (`go test ./...`) |
| `go test ./internal/parser` | 단일 패키지 |
| `go test -run TestEndToEnd_HappyPath ./test/integration` | 단일 e2e 케이스 |
| `go test -race ./...` | race detector |
| `go test -count=10 ./...` | flaky 검출 |

## Unit {#unit}

| 패키지 | 검증 대상 |
|---|---|
| `parser` | 키워드 미스 → `ErrNoMatch`, separator 누락 / 상대경로 → `ErrMalformed`, 정상 → `Event` |
| `discovery` | 패턴 매칭, 날짜 윈도우 (오늘 / 어제+grace / 그 외), `Now` 주입으로 자정 경계 검증 |
| `tailer` | partial line 합치기, EOF→다음 read, inode rotation 시 reopen + offset 0 |
| `writer` | 채널 close 시 자연 종료, `WriteRecord` 에러 전파 |
| `catalog` | `Open` (PRAGMA, DDL), `WriteRecord` (단일 txn 원자성), `GetOffset` / `ErrNoOffset` |

테스트 파일은 같은 디렉토리에 `*_test.go` 로 둔다.

## Integration {#integration}

| 케이스 | 검증 |
|---|---|
| `TestEndToEnd_HappyPath` | 5건 append → 5초 내 5 row commit |
| `TestEndToEnd_ResumesAfterRestart` | 3건 처리 → graceful stop → 4건 추가 → 재기동 후 총 7 row + 순서 보존 |

테스트는 `t.TempDir` 의 임시 로그 디렉토리 + 임시 DB 를 사용. discovery / tail interval 을 50ms / 20ms 로 줄여 빠르게 수렴시킨다.

`waitForRows(t, dbPath, want, timeout)` helper 가 50ms 폴링으로 카운트 도달까지 대기 — race 없는 동기화 포인트.

## 새 e2e 케이스 추가 시

1. `t.TempDir` 로 logDir + dbPath 확보.
2. `app.Run` 을 goroutine 으로 기동, `done := make(chan error, 1)` 로 종료 수신.
3. ETL 로그 라인 직접 append (`os.OpenFile` + `O_APPEND`).
4. `waitForRows` 로 동기화.
5. `cancel()` → `<-done` 으로 graceful 종료 검증.

`Pattern` 은 PRD 의 default 정규식을 그대로 사용해 운영 동작과 동등성을 확보.

## 빌드 검증 {#build-verify}

| 명령 | 의도 |
|---|---|
| `make vet` | `go vet ./...` |
| `make build` | host 빌드 (개발용) |
| `make build-linux` | 배포 빌드 — RHEL 6.10 호환성 |

배포 빌드는 CI 가 아니더라도 PR 직전에 한 번은 수행해 cross-build 깨짐을 잡는다.

## 다루지 않는 것 {#non-goals}

- 부하 테스트 — 운영 머신에서 사내 검증 (`PRD.txt` §13).
- `kill -9` 후 0-loss / 0-dup 검증의 자동화 — MVP 미구현. 수동 검증으로 대체.
- 컨테이너 기반 RHEL 6 호환 e2e — MVP 미구현.
