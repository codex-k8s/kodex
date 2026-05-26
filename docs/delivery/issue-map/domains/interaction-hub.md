---
doc_id: MAP-CK8S-DOMAIN-INTERACTION-HUB
type: issue-map
title: kodex — карта Issue домена взаимодействий
status: active
owner_role: KM
created_at: 2026-04-25
updated_at: 2026-05-26
---

# Карта Issue — центр взаимодействий

## TL;DR

Долгоживущая карта домена `interaction-hub`.

## Матрица

| Issue/PR | Документы | Волна | Статус | Примечание |
|---|---|---|---|---|
| #582 | `docs/domains/interaction-hub/README.md`<br>`docs/domains/interaction-hub/product/requirements.md`<br>`docs/domains/interaction-hub/architecture/design.md`<br>`docs/domains/interaction-hub/architecture/data_model.md`<br>`docs/domains/interaction-hub/architecture/api_contract.md`<br>`docs/domains/interaction-hub/delivery/interaction_hub_delivery.md` | wave 13 | docs-first | Стартовый пакет домена: feedback, approval, Human gate, уведомления, подписки, delivery attempts, callback и гибридная channel model без реализации кода, proto, AsyncAPI или OpenAPI. |
| #768 | `proto/kodex/interactions/v1/interaction_hub.proto`<br>`proto/gen/go/kodex/interactions/v1/**`<br>`specs/asyncapi/interaction-hub.v1.yaml`<br>`libs/go/platformevents/interaction/events.gen.go`<br>`libs/go/accesscatalog/actions.go`<br>`docs/domains/interaction-hub/architecture/api_contract.md`<br>`docs/domains/interaction-hub/delivery/interaction_hub_delivery.md`<br>`specs/README.md`<br>`specs/asyncapi/README.md` | IH-1 | contracts-ready | Контрактный срез: gRPC, AsyncAPI, события `interaction.*`, действия доступа и stable channel delivery/callback DTO без сервисной реализации, БД, миграций, gateway OpenAPI и конкретных внешних каналов. |
| #783 | `services/internal/interaction-hub/**`<br>`services.yaml`<br>`docs/domains/interaction-hub/architecture/api_contract.md`<br>`docs/domains/interaction-hub/delivery/interaction_hub_delivery.md`<br>`docs/delivery/coordination/agent-4-interaction-hub.md` | IH-2 | service-scaffold | Сервисный каркас: composition root, env config, health/readiness/metrics, gRPC registration, domain service skeleton и repository stub; операции возвращают `Unimplemented`, БД/миграции/channel adapters/gateway не входят. |
| #800 | `docs/domains/interaction-hub/architecture/data_model.md`<br>`docs/domains/interaction-hub/architecture/design.md`<br>`docs/domains/interaction-hub/product/requirements.md`<br>`docs/domains/interaction-hub/delivery/interaction_hub_delivery.md`<br>`docs/delivery/coordination/agent-4-interaction-hub.md`<br>`services/internal/interaction-hub/**`<br>`services.yaml` | IH-3 | persistence-foundation | PostgreSQL-модель, repository для thread/message MVP lifecycle, command result idempotency, service-local outbox и синхронизация документации с proto `service` scope и `*_policy_ref`. |
| #806 | `services/internal/interaction-hub/**`<br>`docs/domains/interaction-hub/architecture/api_contract.md`<br>`docs/domains/interaction-hub/delivery/interaction_hub_delivery.md`<br>`docs/delivery/coordination/agent-4-interaction-hub.md` | IH-4 | request-lifecycle | Feedback, approval и Human gate lifecycle реализован через PostgreSQL repository и service-local outbox: create/get/list, response, cancel, expire и command idempotency без external channel adapters и без владения business decision state. |
| #821 | `proto/kodex/interactions/v1/interaction_hub.proto`<br>`proto/gen/go/kodex/interactions/v1/**`<br>`services/internal/interaction-hub/**`<br>`docs/domains/interaction-hub/architecture/api_contract.md`<br>`docs/domains/interaction-hub/architecture/data_model.md`<br>`docs/domains/interaction-hub/delivery/interaction_hub_delivery.md`<br>`docs/delivery/coordination/agent-4-interaction-hub.md` | IH-5a | notification-subscription-lifecycle | Notification/subscription lifecycle реализован без delivery attempts и без hardcoded внешних каналов: request notification, create/update/disable/list subscription, idempotency, optimistic concurrency, safe refs/status/policy refs и outbox events. |
| #835 | `services/internal/interaction-hub/**`<br>`docs/domains/interaction-hub/architecture/api_contract.md`<br>`docs/domains/interaction-hub/architecture/data_model.md`<br>`docs/domains/interaction-hub/architecture/design.md`<br>`docs/domains/interaction-hub/delivery/interaction_hub_delivery.md`<br>`docs/delivery/coordination/agent-4-interaction-hub.md` | IH-5b | delivery-attempt-lifecycle | Delivery attempt lifecycle реализован без hardcoded внешних каналов: `PlanDelivery`, `RecordDeliveryResult`, `GetDeliveryStatus`, safe refs/status/retry metadata, state machine и outbox events. |
| не назначено | `docs/domains/interaction-hub/delivery/interaction_hub_delivery.md` | IH-6+ | planned | Следующие срезы должны выделяться в отдельные Issue: channel contract integration, callback, MCP и ops. |
