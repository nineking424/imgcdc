# Deployment

빌드부터 RHEL 6.10 배포까지의 절차.

타겟 OS 는 RHEL 6.10 (kernel 2.6.32, glibc 2.12). 그래서 빌드는 `CGO_ENABLED=0` 로 잠겨있다.

## Install {#install}

운영자는 직접 빌드하지 않는다. GitHub Releases 에서 사전 빌드된 정적 바이너리를 받는다.

| 플랫폼 | 아카이브 |
|---|---|
| RHEL 6.10 / 모든 linux x86_64 | `imgcdc_<version>_linux_amd64.tar.gz` |
| linux ARM64 (Graviton 등) | `imgcdc_<version>_linux_arm64.tar.gz` |
| macOS Intel (개발용) | `imgcdc_<version>_darwin_amd64.tar.gz` |
| macOS Apple Silicon (개발용) | `imgcdc_<version>_darwin_arm64.tar.gz` |

설치 (RHEL 6.10 예시):

```sh
VERSION=0.1.0
curl -LO https://github.com/nineking424/imgcdc/releases/download/v${VERSION}/imgcdc_${VERSION}_linux_amd64.tar.gz
curl -LO https://github.com/nineking424/imgcdc/releases/download/v${VERSION}/imgcdc_${VERSION}_checksums.txt
sha256sum -c imgcdc_${VERSION}_checksums.txt --ignore-missing
tar xzf imgcdc_${VERSION}_linux_amd64.tar.gz imgcdc
sudo install -m 0755 imgcdc /usr/local/bin/imgcdc
/usr/local/bin/imgcdc --version
```

`--version` 출력으로 설치된 버전/커밋/빌드 시각을 확인할 수 있다.

## Build from source (optional) {#build}

배포 빌드는 CI 가 처리하지만, 디버깅용 로컬 빌드는 다음 명령으로 가능하다.

| 용도 | 명령 | 산출물 |
|---|---|---|
| 로컬 (host OS) | `make build` | `bin/imgcdc` (host platform) |
| 배포 동등 (RHEL 6.10) | `make build-linux` | `bin/imgcdc` (linux/amd64, static) |
| GoReleaser dry-run | `make snapshot` | `dist/` 아래 4 플랫폼 아카이브 |

`build-linux` 은 다음을 실행한다 (`Makefile`):

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -ldflags="-s -w -X main.version=... -X main.commit=... -X main.date=..." \
  -o bin/imgcdc ./cmd/imgcdc
```

검증:

```sh
file bin/imgcdc
# ELF 64-bit LSB executable, x86-64, ..., statically linked
```

산출물은 ~10–15 MB. glibc 의존성 0 — RHEL 6.10 부터 최신 배포판까지 동일 바이너리로 동작.

## Cutting a release {#cutting-a-release}

릴리즈는 git tag push 로 트리거된다.

```sh
# 1. main 이 깨끗하고 최신인지 확인
git checkout main && git pull --ff-only

# 2. semver 태그 생성 (annotated 권장)
git tag -a v0.1.0 -m "v0.1.0"

# 3. 푸시 — `release` 워크플로우가 발동
git push origin v0.1.0
```

워크플로우가 끝나면 `https://github.com/nineking424/imgcdc/releases/tag/v0.1.0` 에 4개 아카이브 + checksums 파일이 게시된다.

**Pre-release**: 태그에 `-` 가 들어가면 (`v0.1.0-rc.1`) GoReleaser 가 자동으로 GitHub pre-release 로 마킹한다.

**Dry-run**: 태그를 만들지 않고 워크플로우만 시험하려면 GitHub UI 의 *Actions → release → Run workflow* 에서 `dry_run=true` 로 실행. 결과 아카이브는 워크플로우 artifacts 에 7일간 보존된다.

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
| 운영자가 잘못된 OS 바이너리를 다운로드 | `darwin_amd64.tar.gz` 를 RHEL 에 풀면 `cannot execute binary file`. 항상 `linux_amd64` 를 받는다 |
