---
id: SVC-MC-013
title: control-api-gateway
type: service
status: approved
owner: developer
version: 2.0.0
updated: 2026-08-23
---

# control-api-gateway

Owner-facing HTTP/WebSocket boundary для production Control Center.

## Ответственность

- проверяет OIDC/browser session, exact Origin, CSRF и rate limits;
- требует semantic `Idempotency-Key` и `If-Match` для применимых mutations;
- преобразует OpenAPI requests в generated control-plane gRPC clients;
- нормализует typed domain errors в стабильный HTTP/error contract;
- авторизует Run stream через тот же control-plane owner rule, что HTTP;
- доставляет authoritative graph snapshot и ordered resumable deltas.

Gateway не читает PostgreSQL, не вычисляет permissions, lifecycle, terminal
state или `nextActions`, не владеет event store и не обращается к Mattermost.
Actor/organization/project/lineage не принимаются из browser payload.

## Realtime

`WSS /api/v1/runs/{runRef}/stream` сначала отдаёт snapshot + current sequence,
затем bounded `RunEvent` deltas. Client передаёт `afterSequence`; duplicate
игнорируется, gap восстанавливается catch-up либо новым snapshot. Raw stdout,
stderr, Codex JSONL, provider payload, secret и file body запрещены.

## Локализация ошибок

Backend возвращает stable code/message key и безопасные parameters. PWA выбирает
текст из RU/EN YAML по доверенной locale пользователя. Gateway не возвращает
raw downstream message или stack trace.

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

```bash
cd services/external/control-api-gateway
GOWORK=off go test ./...
```
