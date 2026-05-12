---
doc_id: MAP-CK8S-DOMAIN-AGENT-ORCHESTRATION
type: issue-map
title: kodex — карта Issue домена оркестрации агентов
status: active
owner_role: KM
created_at: 2026-04-25
updated_at: 2026-05-12
---

# Карта Issue — оркестрация агентов

## TL;DR

Долгоживущая карта домена `agent-orchestration`.

## Матрица

| Issue/PR | Документы | Волна | Статус | Примечание |
|---|---|---|---|---|
| #733 | `docs/domains/agent-orchestration/product/requirements.md`, `docs/domains/agent-orchestration/architecture/design.md`, `docs/domains/agent-orchestration/architecture/data_model.md`, `docs/domains/agent-orchestration/architecture/api_contract.md`, `docs/domains/agent-orchestration/delivery/agent_manager_delivery.md` | AGO-0 | готово | Стартовый доменный пакет документации: границы `agent-manager`, flow, stage, role, prompt, session, run, acceptance, follow-up и междоменные интеграции. |
| не назначено | `proto/kodex/agents/**`, `specs/asyncapi/agent-manager.v1.yaml`, `libs/go/platformevents/**`, `libs/go/accesscatalog/**`, `docs/domains/agent-orchestration/**` | AGO-1 | запланировано | Контракты `agent-manager`, события `agent.*` и действия доступа. |
