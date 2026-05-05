# imgcdc

ETL 처리 로그를 polling-tail 해 info 파일 출현 이벤트를 SQLite WAL catalog 로 기록하는 단일 정적 ELF 데몬.

대상 환경은 RHEL 6.10 (kernel 2.6.32). fanotify / inotify 사용 불가 정책 하에서 ETL 처리 로그(`DEFECTIMG.PARSE.OK` 라인)가 유일한 이벤트 1차 소스다. 자세한 설계 배경은 저장소 루트의 `PRD.txt` 를 본다.

## 페르소나별 시작점

| 누구 | 무엇을 하려는가 | 시작 페이지 |
|---|---|---|
| Operator | 데몬을 빌드/배포/운영한다 | [operating/configuration.md](operating/configuration.md) → [operating/deployment.md](operating/deployment.md) |
| Operator | 장애 / 의심 동작을 진단한다 | [operating/troubleshooting.md](operating/troubleshooting.md) |
| Operator | 재기동 / 회전 / 복구 절차가 필요하다 | [operating/runbook.md](operating/runbook.md) |
| Consumer 개발자 | catalog.db 를 읽어 후속 처리한다 | [consumer/guide.md](consumer/guide.md) |
| Consumer 개발자 | 어떤 이벤트가 어떤 보증으로 들어오는지 알고 싶다 | [concepts/catalog-schema.md](concepts/catalog-schema.md) |
| Contributor | 데이터 흐름과 컴포넌트 책임을 이해한다 | [concepts/architecture.md](concepts/architecture.md) |
| Contributor | 코드를 수정한다 | [developer/code-map.md](developer/code-map.md) → [developer/testing.md](developer/testing.md) |

## 의도적으로 다루지 않는 것

- 메트릭 / 관측 (운영 정책상 미제공)
- HTTP API / RPC
- 파일 DELETE / MODIFY 이벤트 (소스 부재 + immutable 가정)
- 카탈로그 retention cleanup (MVP 미구현)
- 디렉토리 traversal / cold-start 스캔

각 항목의 사유는 `PRD.txt` §15 (EXPLICITLY EXCLUDED) 에 기록된다.
