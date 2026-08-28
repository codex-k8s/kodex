---
id: ARCH-MC-008
title: Вложения и artifacts
type: architecture
status: approved
owner: architect
version: 2.0.0
updated: 2026-08-28
---

# Вложения и artifacts

## Владение и хранение

Control-plane владеет `Artifact`, scan/lifecycle, bindings, retention, result
relation и download grants. PostgreSQL хранит tenant-scoped metadata и точный
S3 receipt; bounded immutable body хранится только в обязательном
S3-compatible bucket. Content не помещается в audit, outbox, NATS, WebSocket
или PostgreSQL `bytea`.

Server-assigned object key имеет форму
`organizations/<organizationRef>/projects/<projectRef>/artifacts/<artifactRef>/<sha256>`.
Ни один внешний actor не выбирает key, bucket, version или owner scope.

Local hot-reload использует SeaweedFS 4.41 в `kodex-system`. Production
использует заранее подготовленный внешний S3 endpoint и bucket через Kubernetes
Secret. Это один внутренний storage port и не пользовательская integration.

## Upload

1. Browser отправляет metadata и bounded stream через owner endpoint.
2. Gateway проверяет session/Origin/CSRF/rate/body limits и использует generated
   streaming gRPC client.
3. Control-plane разрешает User и Project, ограниченно читает stream, вычисляет
   digest и выполняет встроенную проверку содержимого.
4. Control-plane загружает тело по server-assigned key, выполняет `HeadObject` и
   принимает только совпадающие digest и size.
5. Одна PostgreSQL transaction фиксирует Artifact metadata, exact S3 receipt,
   audit, idempotency receipt и обязательное событие. При незавершённой
   transaction подготовленный object удаляется bounded cleanup.
6. Только `CLEAN` Artifact может стать input, knowledge binding, preview или
   download. Control Center получает safe metadata event и читает body только
   отдельным request.

S3 upload и PostgreSQL transaction не объявляются общей распределённой
transaction. Fail-closed readback и cleanup prepared object являются явным
компенсирующим контрактом.

## Generated result

Agent-runner завершает execution с bounded result manifest. Control-plane
проверяет claim/fence, digest, declared size/media type, подготавливает objects и
в одной terminal PostgreSQL transaction связывает их с точными
Run/node/turn/attempt. Partial failure удаляет подготовленные objects; broad S3
credentials в role Pod отсутствуют.

## Download

1. Browser запрашивает artifactRef из авторитетного Project/Run readback.
2. Control-plane повторно проверяет organization/project eligibility, scan state
   и retention.
3. S3 receipt разрешается только из PostgreSQL; `GetObject` version/digest/size
   повторно сверяются до выдачи.
4. Download operation выдаёт body bounded chunks; gateway не буферизует файл
   целиком и задаёт безопасные content headers.
5. Filename является недоверенным display metadata; active content не
   исполняется inline без allowlist preview.

## Runtime materialization

Runtime получает только exact Artifact refs из `RuntimeRevision`. Materializer
скачивает их по execution-scoped bearer + mTLS, повторно сверяет size/digest,
пишет в private workspace и не получает S3 endpoint или credentials.

## Readiness и эксплуатационная граница

Control-plane читает endpoint/region/bucket через `secretKeyRef`, а access key и
secret key только из read-only files. Startup выполняет authenticated
`HeadBucket`; отсутствие Secret, endpoint или bucket закрыто останавливает
готовность. Local bucket создаётся отдельной bootstrap Job без вывода
credentials. Production bucket создаётся оператором до запуска release.

PostgreSQL backup без соответствующих S3 objects не восстанавливает artifacts.
Полный backup/retention/restore drill реализует отдельный unit #1003.
Session JSONL не является Artifact body; его S3 archive/restore реализует
отдельный unit #1002.

## Ограничения первой версии

Максимальный размер upload/generated artifact задаётся server policy и не может
быть повышен browser payload. Multipart large objects и range download остаются
вне текущего MVP.
