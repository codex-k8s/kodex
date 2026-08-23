---
id: ARCH-MC-008
title: Вложения и artifacts
type: architecture
status: approved
owner: architect
version: 1.1.0
updated: 2026-08-23
---

# Вложения и artifacts

## Владение и хранение

Control-plane владеет `Artifact`, `ArtifactVersion`, scan/lifecycle, bindings,
retention, result relation и download grants. Fresh web-only baseline хранит
bounded object body в отдельной PostgreSQL таблице под той же organization
boundary. Content не помещается в audit, outbox, NATS или WebSocket.

Архитектурный port не раскрывает тип хранилища. Поздний переход к internal
object storage меняет repository adapter, но не owner API и domain model.

## Upload

1. Browser отправляет metadata и bounded stream через owner endpoint.
2. Gateway проверяет session/Origin/CSRF/rate/body limits и использует generated
   streaming gRPC client.
3. Control-plane разрешает User и Project, ограниченно читает stream, вычисляет
   digest и выполняет встроенную проверку содержимого до открытия транзакции.
4. Fresh baseline атомарно сохраняет metadata, content, audit, idempotency
   receipt и `artifact.available`/platform invalidation с итоговым persisted
   scan state `CLEAN`, `QUARANTINED` либо `FAILED`. Это синхронный bounded
   inspection, а не фиктивный background scanner; только `CLEAN` version может
   стать input, knowledge binding, preview или download.
5. Control Center получает safe metadata event и читает body только отдельным
   download/preview request.

## Generated result

Agent-runner завершает execution с bounded result manifest. Control-plane
проверяет claim/fence, digest, declared size/media type и связывает Artifact с
точными Run/node/turn/attempt в той же terminal transaction. Произвольный путь
role Pod или provider response не становится storage locator.

## Download

1. Browser запрашивает artifactRef из авторитетного Project/Run readback.
2. Control-plane повторно проверяет organization/project eligibility, scan state
   и retention.
3. Download operation выдаёт body bounded chunks; gateway не буферизует файл
   целиком и задаёт безопасные content headers.
4. Filename кодируется как недоверенное display metadata, active content не
   исполняется inline без allowlist preview.

## Runtime materialization

Runtime получает только exact ArtifactVersion refs из RuntimeRevision.
Materializer скачивает их по execution-scoped bearer + mTLS, повторно сверяет
size/digest, пишет в private workspace и не получает broad database/storage
credential.

## Optional delivery

Interaction adapter читает Artifact тем же ограниченным owner-approved path и
фиксирует отдельный DeliveryAttempt. Его outage/retry не меняет Artifact state и
terminal core Run. External post/thread ID остаётся только delivery metadata.

## Ограничения первой версии

Максимальный размер upload/generated artifact задаётся server policy и не может
быть повышен browser payload. Multipart large objects, range download и внешний
S3 backend относятся к POST-MVP, но domain contract уже допускает смену storage
adapter.
