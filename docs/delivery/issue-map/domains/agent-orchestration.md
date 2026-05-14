---
doc_id: MAP-CK8S-DOMAIN-AGENT-ORCHESTRATION
type: issue-map
title: kodex — карта Issue домена оркестрации агентов
status: active
owner_role: KM
created_at: 2026-04-25
updated_at: 2026-05-14
---

# Карта Issue — оркестрация агентов

## TL;DR

Долгоживущая карта домена `agent-orchestration`.

## Матрица

| Issue/PR | Документы | Волна | Статус | Примечание |
|---|---|---|---|---|
| #733 | `docs/domains/agent-orchestration/product/requirements.md`, `docs/domains/agent-orchestration/architecture/design.md`, `docs/domains/agent-orchestration/architecture/data_model.md`, `docs/domains/agent-orchestration/architecture/api_contract.md`, `docs/domains/agent-orchestration/delivery/agent_manager_delivery.md` | AGO-0 | готово | Стартовый доменный пакет документации: границы `agent-manager`, flow, stage, role, prompt, session, run, acceptance, follow-up и междоменные интеграции. |
| #739 | `proto/kodex/agents/v1/agent_manager.proto`, `proto/gen/go/kodex/agents/v1/**`, `specs/asyncapi/agent-manager.v1.yaml`, `libs/go/platformevents/agent/**`, `libs/go/accesscatalog/**`, `docs/domains/agent-orchestration/**` | AGO-1 | готово | Контракты `agent-manager`, события `agent.*` и действия доступа готовы; сервисный код, БД, миграции и deploy не входят в срез. |
| #698 | `docs/platform/architecture/codex_hooks_and_skills.md` | architecture | на рассмотрении | Вариантная проработка использования Codex hooks и skills; Issue не закрывается до выбора целевой модели владельцем. |
