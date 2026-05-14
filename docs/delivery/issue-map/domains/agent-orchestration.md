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
| #698 | `docs/platform/architecture/codex_hooks_and_skills.md` | architecture | решение выбрано | До MVP выбран минимальный слой hooks со сбором всех поддерживаемых событий для realtime UI; skills остаются после MVP через отдельный слой управляемых возможностей. |
| #744 | `services/internal/agent-manager/**`, `docs/domains/agent-orchestration/delivery/agent_manager_delivery.md`, `docs/domains/agent-orchestration/architecture/api_contract.md` | AGO-2 | готово | Сервисный каркас `agent-manager`: process bootstrap, env-конфигурация, health/readiness/metrics, gRPC registration и outbox skeleton без БД, миграций, deploy и бизнес-операций. |
| #749 | `services/internal/agent-manager/**`, `docs/domains/agent-orchestration/architecture/data_model.md`, `docs/domains/agent-orchestration/delivery/agent_manager_delivery.md` | AGO-3 | готово | PostgreSQL-модель flow, stage, role, prompt template, версий, command result и service-local outbox; storage/use-case слой готов, gRPC handler wiring вынесен в следующий срез. |
| #281, #282 | `docs/platform/architecture/repository_onboarding.md`, `docs/domains/agent-orchestration/**` | междоменное решение | модель выбрана, ждёт реализации | Выбран вариант C. `agent-manager` должен запускать bootstrap/adoption роли и детерминированные запуски по шаблону, готовить отчёт и PR через provider-контур, но не владеть проектной политикой, пакетами или файловой системой workspace. |
| #747 | `docs/domains/platform-mcp-server/**`, `docs/platform/architecture/mcp_and_interaction_model.md`, `docs/platform/architecture/codex_hooks_and_skills.md` | MCP-0 | готово | `platform-mcp-server` зафиксирован как инструментальная поверхность для будущих agent-manager tools и hooks; `Run`, session, flow, role, prompt и gates остаются у `agent-manager`. |
