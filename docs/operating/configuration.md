# Configuration

모든 설정은 CLI 인자다. 환경변수 / 설정파일 미지원 (`PRD.txt` D-009).

소스: `cmd/imgcdc/main.go` 의 `flag.String` / `flag.Duration` 호출.

## Required {#required}

| 플래그 | 설명 |
|---|---|
| `--log-dir <path>` | ETL 로그 디렉토리. 예: `/var/log/etl` |
| `--db <path>` | SQLite catalog 파일 경로. 예: `/var/lib/imgcdc/catalog.db` |

둘 중 하나라도 비면 exit 2 + usage 출력.

## Optional {#optional}

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `--file-pattern <regex>` | `^etl_defectimg_work\d+_\d{4}_\d{2}_\d{2}\.log$` | 디렉토리 안에서 매칭할 파일명 정규식 |
| `--keyword <string>` | `DEFECTIMG.PARSE.OK` | 라인에 포함되어야 매칭으로 인정 |
| `--path-separator <string>` | ` - ` (공백-하이픈-공백) | real path 추출용 구분자 |
| `--discovery-interval <dur>` | `1s` | 디렉토리 ReadDir 주기 |
| `--tail-interval <dur>` | `1s` | EOF 시 다음 read 까지 대기 |
| `--grace <dur>` | `90m` | 자정 이후 어제 날짜 파일을 추가로 따라가는 시간 |
| `--shutdown-timeout <dur>` | `5s` | SIGTERM 후 강제 종료까지의 대기 |
| `--log-level <str>` | `info` | `debug` / `info` / `warn` / `error` |

duration 은 Go `time.ParseDuration` 형식 (`500ms`, `2h`, `90m`).

## Grace 의 의미 {#grace}

ETL 작업 1건의 처리 시간 한도가 1시간이다. 자정 직전에 시작된 작업이 자정 이후 라인을 *전날* 파일에 쓰는 케이스를 잡기 위해 기본 90분 (1h 한도 + 30m 안전 마진).

`grace` 가 너무 짧으면 자정 직후 라인을 놓칠 수 있다. 너무 길면 retire 가 지연될 뿐 데이터 손실은 없다 — 안전 쪽으로 길게 둔다.

## Examples {#examples}

기본값으로 기동:

```sh
imgcdc \
  --log-dir /var/log/etl \
  --db /var/lib/imgcdc/catalog.db
```

폴링 주기와 grace 를 조정:

```sh
imgcdc \
  --log-dir /var/log/etl \
  --db /var/lib/imgcdc/catalog.db \
  --grace 2h \
  --tail-interval 500ms \
  --discovery-interval 2s
```

디버깅 (라인별 trace 확인):

```sh
imgcdc --log-dir ./tmp/log --db ./tmp/c.db --log-level debug
```

## SIGHUP / 동적 reload {#reload}

미지원. 설정 변경은 supervisor 가 데몬을 재시작하는 식으로 한다. 재시작은 [runbook.md#1-graceful-restart](runbook.md#1-graceful-restart) 절차로 진행한다.
