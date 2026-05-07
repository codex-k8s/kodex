---
doc_id: MAP-CK8S-DOMAIN-RUNTIME-AND-FLEET
type: issue-map
title: kodex — карта Issue домена runtime и fleet
status: active
owner_role: KM
created_at: 2026-04-25
updated_at: 2026-05-07
---

# Карта Issue — runtime и контур серверов и кластеров

## TL;DR

Долгоживущая карта домена `runtime-and-fleet`. `runtime-manager` владеет слотами, workspace materialization и platform jobs. `fleet-manager` владеет серверами, Kubernetes-кластерами и placement scope. Agent `Run` принадлежит `agent-manager`, а не runtime.

## Матрица

| Issue/PR | Документы | Волна | Статус | Примечание |
|---|---|---|---|---|
| #655 | `docs/domains/runtime-and-fleet/product/requirements.md`, `docs/domains/runtime-and-fleet/architecture/design.md`, `docs/domains/runtime-and-fleet/architecture/data_model.md`, `docs/domains/runtime-and-fleet/architecture/api_contract.md`, `docs/domains/runtime-and-fleet/delivery/runtime_manager_delivery.md` | RTM-0 | готово | Доменная документация, границы runtime/fleet, карта Issue и план поставки. |
| #656 | `proto/kodex/runtime/v1/runtime_manager.proto`, `specs/asyncapi/runtime-manager.v1.yaml`, `libs/go/platformevents/runtime/**`, `docs/domains/runtime-and-fleet/architecture/api_contract.md` | RTM-1 | запланировано | gRPC и AsyncAPI контракты `runtime-manager`. |
| #657 | `services/internal/runtime-manager/**`, `docs/domains/runtime-and-fleet/architecture/data_model.md`, `docs/domains/runtime-and-fleet/delivery/runtime_manager_delivery.md` | RTM-2 | запланировано | Каркас сервиса, PostgreSQL-модель, миграции, repository, health/readiness и outbox. |
| #658 | `services/internal/runtime-manager/**`, `docs/domains/runtime-and-fleet/architecture/design.md` | RTM-3 | запланировано | Жизненный цикл слотов: reserve, extend lease, release, fail и MVP default cluster boundary. |
| #659 | `services/internal/runtime-manager/**`, `docs/domains/runtime-and-fleet/architecture/design.md` | RTM-4 | запланировано | Workspace materialization: source refs, access mode, local paths, fingerprint и ошибки подготовки. |
| #660 | `services/internal/runtime-manager/**`, `docs/domains/runtime-and-fleet/architecture/data_model.md` | RTM-5 | запланировано | Platform job MVP: job/step state machine, short log tail, full log ref и executor boundary. |
| #661 | `deploy/**`, `services.yaml`, `services/internal/runtime-manager/**`, `docs/domains/runtime-and-fleet/delivery/runtime_manager_delivery.md` | RTM-6 | запланировано | Эксплуатационный контур: Dockerfile, manifests, DB bootstrap, migration job, smoke и runbook. |
| #662 | `services/internal/runtime-manager/**`, `docs/domains/runtime-and-fleet/architecture/design.md` | RTM-7 | запланировано | Cleanup, retention, prewarm pool и deterministic reuse. |
