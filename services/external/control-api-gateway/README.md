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
- авторизует каждую Run subscription через тот же control-plane owner rule,
  что HTTP;
- одним owner session socket доставляет platform invalidations, authoritative
  graph snapshots и ordered resumable Run deltas.

Gateway не читает PostgreSQL, не вычисляет permissions, lifecycle, terminal
state или `nextActions`, не владеет event store и не обращается к Mattermost.
Actor/organization/project/lineage не принимаются из browser payload.

## Realtime

`WSS /api/v1/session/stream` является единственным browser realtime transport.
Client передаёт platform cursor и bounded список Run cursors, а затем динамически
подписывает и отписывает Runs в том же socket. Каждая Run получает собственные
snapshot/catch-up/deltas и восстанавливает gap независимо от остальных потоков.
Duplicate игнорируется, slow client закрывается по bounded backpressure и
возобновляется с сохранённых cursors. Raw stdout, stderr, Codex JSONL, provider
payload, secret и file body запрещены.

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

```bash
cd services/external/control-api-gateway
GOWORK=off go test ./...
```
