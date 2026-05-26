# integration-gateway

## Назначение

`integration-gateway` — тонкий HTTP-вход для внешних webhook и callback событий.

Это не домен-владелец бизнес-состояния. Пакет размещён в `docs/domains/**`, потому что у сервиса есть самостоятельная граница, HTTP-контракты, поставка и эксплуатация, но сквозная архитектура продолжает считать его пограничным компонентом.

## Что входит

- Публичные HTTP endpoints для внешних webhook и callback событий.
- Проверка источника, подписи, размера payload, лимитов и backpressure на границе.
- Очистка и безопасная нормализация edge envelope.
- Маршрутизация проверенного события во внутренний сервис-владелец по gRPC.
- Первый MVP-маршрут: provider webhook -> `provider-hub.IngestWebhookEvent`.
- Минимальный runnable-каркас сервиса: process/config/graceful shutdown, health/readiness/metrics,
  HTTP router, OpenAPI runtime validation и provider-hub client interface.
- Активный GitHub provider webhook route с source binding, HMAC SHA-256 проверкой подписи и безопасной ссылкой на webhook secret.
- Ограниченная диагностика входящего контура без сырых секретов, больших payload и бизнес-проекций.

## Что не входит

- Provider projections, webhook inbox, provider cursors, операции провайдера и нормализация provider business events — зона `provider-hub`.
- `Run`, session, flow, role, prompt и acceptance — зона `agent-manager`.
- Диалоги, delivery attempts, уведомления и состояние внешних callback delivery — зона `interaction-hub`.
- Risk/gate/release decisions — зона `governance-manager`.
- Codex hook events — зона `codex-hook-ingress`.
- MCP tools — зона `platform-mcp-server`.
- UI endpoints для сотрудников или внешних пользователей — зоны `staff-gateway` и `user-gateway`.

## Документы

| Документ | Путь |
|---|---|
| Требования | `product/requirements.md` |
| Дизайн | `architecture/design.md` |
| API-обзор | `architecture/api_contract.md` |
| План поставки | `delivery/integration_gateway_delivery.md` |

## Спецификации

| Спецификация | Путь |
|---|---|
| OpenAPI gateway-поверхности | `../../../specs/openapi/integration-gateway.v1.yaml` |

## Карта Issue

- Карта сервисного пакета: `docs/delivery/issue-map/domains/integration-gateway.md`.
