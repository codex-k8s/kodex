---
doc_id: MAP-CK8S-DOMAIN-INTEGRATION-GATEWAY
type: issue-map
title: kodex — карта Issue integration-gateway
status: active
owner_role: KM
created_at: 2026-05-25
updated_at: 2026-05-26
---

# Карта Issue — integration-gateway

## Кратко

Карта сервисного пакета `integration-gateway`. Сервис принимает внешние webhook и callback HTTP-события, проверяет их на границе и маршрутизирует во внутренние сервисы-владельцы без владения бизнес-состоянием.

## Матрица

| Issue/PR | Документы | Срез | Статус | Примечание |
|---|---|---|---|---|
| #781 | `docs/domains/integration-gateway/**`, `specs/openapi/integration-gateway.v1.yaml`, `docs/platform/architecture/c4_container.md`, `docs/platform/architecture/service_boundaries.md`, `docs/platform/architecture/provider_integration_model.md`, `docs/delivery/coordination/agent-2-provider-hub.md` | IGW-0 | готово | Зафиксированы требования и границы `integration-gateway`, первый MVP route provider webhook -> `provider-hub.IngestWebhookEvent`, security/backpressure/retry/idempotency требования и OpenAPI-каркас. Код сервиса не входит. |
| #792 | `services/external/integration-gateway/**`, `tools/codegen/openapi/integration-gateway.oapi-codegen.yaml`, `services.yaml`, `docs/domains/integration-gateway/**`, `docs/delivery/coordination/agent-2-provider-hub.md`, `docs/delivery/issue-map/domains/integration-gateway.md` | IGW-1 | готово | Добавлен runnable service scaffold без бизнес-состояния: process/config/graceful shutdown, health/readiness/metrics, HTTP router, OpenAPI validation/generated models, safe middleware и provider-hub client interface; provider route остаётся отключённым stub до verifier-среза. |
| #807 | `services/external/integration-gateway/**`, `specs/openapi/integration-gateway.v1.yaml`, `tools/codegen/openapi/integration-gateway.oapi-codegen.yaml`, `services.yaml`, `docs/domains/integration-gateway/**`, `docs/delivery/coordination/agent-2-provider-hub.md`, `docs/delivery/issue-map/domains/integration-gateway.md` | IGW-2 | готово | Активирован GitHub provider webhook route: gateway проверяет `provider_slug=github`, обязательные GitHub headers, HMAC SHA-256 подпись через safe secret ref, payload limit и маршрутизирует safe envelope в `provider-hub.IngestWebhookEvent` без хранения gateway state. |
| #819 | `services/external/integration-gateway/**`, `services.yaml`, `docs/domains/integration-gateway/**`, `docs/delivery/coordination/agent-2-provider-hub.md`, `docs/delivery/issue-map/domains/integration-gateway.md` | IGW-4 | готово | Усилен публичный GitHub webhook вход: per-route/per-source in-memory limits, backpressure с `Retry-After`, safe audit summary и тесты replay/idempotency/OpenAPI compatibility без gateway БД, inbox или provider business logic. |
