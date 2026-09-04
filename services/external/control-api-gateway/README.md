---
id: SVC-MC-013
title: control-api-gateway
type: service
status: approved
owner: developer
version: 2.1.0
updated: 2026-09-04
---

# control-api-gateway

Owner-facing HTTP/WebSocket boundary для production Control Center.

## Ответственность

- проверяет OIDC/browser session, exact Origin, CSRF и rate limits;
- требует semantic `Idempotency-Key` и `If-Match` для применимых mutations;
- преобразует OpenAPI requests в generated control-plane gRPC clients;
- потоково передаёт bounded multipart audio в `stt-tts-service` через
  generated `sttapi`, не буферизуя запись целиком;
- нормализует typed domain errors в стабильный HTTP/error contract;
- авторизует каждую Run subscription через тот же control-plane owner rule,
  что HTTP;
- одним owner session socket доставляет platform invalidations, authoritative
  graph snapshots и ordered resumable Run deltas.

Gateway не читает PostgreSQL, не вычисляет permissions, lifecycle, terminal
state или `nextActions`, не владеет event store и не обращается к Mattermost.
Actor/organization/project/lineage не принимаются из browser payload.

## HTTP lifecycle

- `GET /api/v1/search` передаёт server-side `projectRef`, `limit` и opaque
  `pageToken`; 500 ms debounce принадлежит только PWA;
- `PUT /api/v1/projects/{projectRef}/agents/{agentRef}/avatar` вызывает
  атомарный streaming `UploadAgentAvatar`, который фиксирует Artifact, binding
  и Agent version в одной owner transaction;
- environment agents/readiness используют только
  `/api/v1/runtime-environments/{environmentRef}/agents` и `/readiness`;
- provider device flow разделён на start, verification и reauthorization, а
  API-key account удаляется отдельным `DELETE /api/v1/provider-accounts/{ref}`;
- `POST /api/v1/projects/{projectRef}/speech/transcriptions` принимает одну
  multipart `audio` part до 25 MiB и возвращает текст с безопасным receipt без
  actor, tenant, credential и provider account metadata.

STT transport проверяет multipart/media type/declared size до открытия
защищённого RPC, отправляет chunks не более 64 KiB последовательно и наследует
request cancellation/deadline. Permission, server-owned policy, credential,
media signature и фактический размер проверяет `stt-tts-service` до provider
effect. Audio, transcript, secret и provider content не записываются в логи.
Direct STT operation зарегистрирована в отдельном `ProofOperations` профиле с
обязательным project scope; дочерние policy/credential RPC получают только
canonical continuation от проверенного parent `platform.stt.transcribe`.

## Realtime

`WSS /api/v1/session/stream` является единственным browser realtime transport.
Client передаёт platform cursor и bounded список Run cursors, а затем динамически
подписывает и отписывает Runs в том же socket. Каждая Run получает собственные
snapshot/catch-up/deltas и восстанавливает gap независимо от остальных потоков.
Duplicate игнорируется, slow client закрывается по bounded backpressure и
возобновляется с сохранённых cursors. Raw stdout, stderr, Codex JSONL, provider
payload, secret и file body запрещены.

PWA продлевает session через межвкладочный lease и передаёт только монотонную
session revision через `BroadcastChannel`. Server renew выполняется только по
explicit `PUT /api/v1/session` после exact Origin и double-submit CSRF и не
превышает expiry исходного bearer. Durable logout revocation выигрывает у
запоздавшего renew, поскольку browser session ID при renew не меняется.

## Локализация ошибок

Backend возвращает stable code/message key и безопасные parameters. Go gateway
выбирает текст из embedded RU/EN YAML по доверенной locale пользователя, а PWA
локализует собственный UI из согласованного RU/EN-каталога. Gateway не
возвращает raw downstream message или stack trace.

## Health/readiness

`/healthz` проверяет process. `/readyz` читает локальный snapshot browser/
transport configuration и прямых sidecars; control-plane, NATS producer,
runtime и optional adapters не вызываются на probe. Их рабочий outage даёт
typed `Unavailable`/HTTP `502/503/504`.

JWKS LKG ограничен двумя минутами без продления на повторной ошибке. Signature,
rollback, conflicting revision и expiry немедленно fail closed.

## Контракты и проверка

- OpenAPI: `contracts/openapi/control-api-gateway/v1/openapi.yaml`;
- WebSocket: `contracts/asyncapi/control-api-gateway/v1/asyncapi.yaml`;
- deploy: `deploy/k8s/base/control-api-gateway`.

Проверенная документация внешних библиотек: gRPC-Go client streaming/flow
control/context cancellation и oidc-client-ts session/events.

```bash
cd services/external/control-api-gateway
GOWORK=off go test ./...
```
