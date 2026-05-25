---
doc_id: MAP-CK8S-DOMAIN-INTERACTION-HUB
type: issue-map
title: kodex — карта Issue домена взаимодействий
status: active
owner_role: KM
created_at: 2026-04-25
updated_at: 2026-05-22
---

# Карта Issue — центр взаимодействий

## TL;DR

Долгоживущая карта домена `interaction-hub`.

## Матрица

| Issue/PR | Документы | Волна | Статус | Примечание |
|---|---|---|---|---|
| #582 | `docs/domains/interaction-hub/README.md`<br>`docs/domains/interaction-hub/product/requirements.md`<br>`docs/domains/interaction-hub/architecture/design.md`<br>`docs/domains/interaction-hub/architecture/data_model.md`<br>`docs/domains/interaction-hub/architecture/api_contract.md`<br>`docs/domains/interaction-hub/delivery/interaction_hub_delivery.md` | wave 13 | docs-first | Стартовый пакет домена: feedback, approval, Human gate, уведомления, подписки, delivery attempts, callback и гибридная channel model без реализации кода, proto, AsyncAPI или OpenAPI. |
| #768 | `proto/kodex/interactions/v1/interaction_hub.proto`<br>`proto/gen/go/kodex/interactions/v1/**`<br>`specs/asyncapi/interaction-hub.v1.yaml`<br>`libs/go/platformevents/interaction/events.gen.go`<br>`libs/go/accesscatalog/actions.go`<br>`docs/domains/interaction-hub/architecture/api_contract.md`<br>`docs/domains/interaction-hub/delivery/interaction_hub_delivery.md`<br>`specs/README.md`<br>`specs/asyncapi/README.md` | IH-1 | contracts-ready | Контрактный срез: gRPC, AsyncAPI, события `interaction.*`, действия доступа и stable channel delivery/callback DTO без сервисной реализации, БД, миграций, gateway OpenAPI и конкретных внешних каналов. |
| не назначено | `docs/domains/interaction-hub/delivery/interaction_hub_delivery.md` | IH-2+ | planned | Следующие срезы должны выделяться в отдельные Issue: сервисный каркас, модель хранения, lifecycle, delivery, channel contract integration, MCP и ops. |
