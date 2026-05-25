---
doc_id: MAP-CK8S-DOMAIN-INTEGRATION-GATEWAY
type: issue-map
title: kodex — карта Issue integration-gateway
status: active
owner_role: KM
created_at: 2026-05-25
updated_at: 2026-05-25
---

# Карта Issue — integration-gateway

## Кратко

Карта сервисного пакета `integration-gateway`. Сервис принимает внешние webhook и callback HTTP-события, проверяет их на границе и маршрутизирует во внутренние сервисы-владельцы без владения бизнес-состоянием.

## Матрица

| Issue/PR | Документы | Срез | Статус | Примечание |
|---|---|---|---|---|
| #781 | `docs/domains/integration-gateway/**`, `specs/openapi/integration-gateway.v1.yaml`, `docs/platform/architecture/c4_container.md`, `docs/platform/architecture/service_boundaries.md`, `docs/platform/architecture/provider_integration_model.md`, `docs/delivery/coordination/agent-2-provider-hub.md` | IGW-0 | готово | Зафиксированы требования и границы `integration-gateway`, первый MVP route provider webhook -> `provider-hub.IngestWebhookEvent`, security/backpressure/retry/idempotency требования и OpenAPI-каркас. Код сервиса не входит. |
