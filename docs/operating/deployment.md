# Deployment

빌드부터 RHEL 6.10 배포까지의 절차.

타겟 OS 는 RHEL 6.10 (kernel 2.6.32, glibc 2.12). 그래서 빌드는 `CGO_ENABLED=0` 로 잠겨있다.

## Build {#build}

| 용도 | 명령 | 산출물 |
|---|---|---|
| 로컬 (host OS) | `make build` | `bin/imgcdc` (host platform) |
| 배포 (RHEL 6.10) | `make build-linux` | `bin/imgcdc` (linux/amd64, static) |

`build-linux` 은 다음을 실행한다 (`Makefile`):

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -ldflags="-s -w" -o bin/imgcdc ./cmd/imgcdc
```

검증:

```sh
file bin/imgcdc
# ELF 64-bit LSB executable, x86-64, ..., statically linked
```

산출물은 ~10–15 MB. glibc 의존성 0 — RHEL 6.10 부터 최신 배포판까지 동일 바이너리로 동작.

## Toolchain

| 항목 | 버전 |
|---|---|
| Go | `go.mod` 의 `go 1.25.6` 기준 (1.22+ 가 PRD 의 최소 라인) |
| SQLite driver | `modernc.org/sqlite v1.50.0` (pure Go, CGO 불요) |
| 그 외 직접 의존 | `golang.org/x/sync v0.20.0` (errgroup) |

## Filesystem 레이아웃 {#filesystem}

| 경로 | 모드 | 용도 |
|---|---|---|
| `/usr/local/bin/imgcdc` | 0755 | 실행 파일 |
| `/var/lib/imgcdc/` | 0750 | catalog DB 디렉토리 |
| `/var/lib/imgcdc/catalog.db` | 0640 | catalog 파일 (writer 가 자동 생성) |
| `/var/lib/imgcdc/catalog.db-wal` | 0640 | SQLite WAL (자동 생성) |
| `/var/lib/imgcdc/catalog.db-shm` | 0640 | SQLite shared memory (자동 생성) |
| `/var/log/etl/` | 외부 권한 | ETL 워커 로그 — imgcdc 는 read 권한만 필요 |

## Permissions {#permissions}

| 주체 | 디렉토리 / 파일 | 권한 |
|---|---|---|
| imgcdc 데몬 user | `/var/log/etl/*.log` | r |
| imgcdc 데몬 user | `/var/lib/imgcdc/` | rwx |
| consumer user | `/var/lib/imgcdc/catalog.db*` | r |

WAL 모드에서 reader 는 `.db` 외에 `.db-shm`, `.db-wal` 까지 읽을 수 있어야 한다. mode 0640 + reader 와 imgcdc 가 같은 group 인 구성이 일반적.

## Upstart 예시 {#upstart}

`/etc/init/imgcdc.conf`:

```
description "imgcdc daemon"
start on runlevel [2345]
stop on runlevel [!2345]
respawn
respawn limit 10 60
exec /usr/local/bin/imgcdc \
     --log-dir /var/log/etl \
     --db /var/lib/imgcdc/catalog.db
```

| 명령 | 동작 |
|---|---|
| `start imgcdc` | 기동 |
| `stop imgcdc` | SIGTERM (graceful, 5s 안에 종료) |
| `restart imgcdc` | stop + start |
| `status imgcdc` | running/stopped 상태 |

`respawn limit 10 60` 은 60초 안에 10번 죽으면 supervisor 가 재시작을 포기한다. 진단은 [troubleshooting.md](troubleshooting.md) 를 본다.

## init.d 스크립트

upstart 가 없는 환경에서는 init.d 스크립트로 동일한 패턴을 구성한다 — daemon 자체는 fork/detach 하지 않고 foreground 단일 프로세스이므로 supervisor 의 `daemon`/`respawn` 옵션에 의존한다.

## 로그 캡처

imgcdc 는 stdout / stderr 에 slog 로 출력한다 (text handler). 로그 라우팅은 supervisor 책임 — upstart 는 기본적으로 `/var/log/upstart/imgcdc.log` 로 redirect.

## Cross-build 시 주의

| 함정 | 원인 / 회피 |
|---|---|
| `bin/imgcdc` 가 macOS 실행 파일이 됨 | `make build` 는 host build. 배포는 반드시 `make build-linux` |
| `cgo` 라이브러리 링크 시도 | `CGO_ENABLED=0` 누락. Makefile target 을 그대로 쓰면 회피됨 |
| 바이너리 크기 증가 | `-ldflags="-s -w"` 누락 (디버그 심볼 포함) |
