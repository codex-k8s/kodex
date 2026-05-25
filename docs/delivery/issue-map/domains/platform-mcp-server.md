---
doc_id: MAP-CK8S-DOMAIN-PLATFORM-MCP-SERVER
type: issue-map
title: kodex — карта Issue platform-mcp-server
status: active
owner_role: KM
created_at: 2026-05-14
updated_at: 2026-05-22
---

# Карта Issue — platform-mcp-server

## TL;DR

Долгоживущая карта сервисного пакета `platform-mcp-server`. Это пограничный MCP-компонент, а не домен-владелец бизнес-состояния.

## Матрица

| Issue/PR | Документы | Срез | Статус | Примечание |
|---|---|---|---|---|
| #747 | `docs/domains/platform-mcp-server/product/requirements.md`, `docs/domains/platform-mcp-server/architecture/design.md`, `docs/domains/platform-mcp-server/architecture/api_contract.md`, `docs/domains/platform-mcp-server/delivery/platform_mcp_server_delivery.md`, `docs/platform/architecture/mcp_and_interaction_model.md`, `docs/platform/architecture/service_boundaries.md`, `docs/platform/architecture/codex_hooks_and_skills.md`, `docs/delivery/coordination/**` | MCP-0 | готово | Границы `platform-mcp-server`, ответственность, MVP-группы инструментов, безопасность и delivery-план. Код, proto и AsyncAPI не входят. |
| #753 | `docs/domains/platform-mcp-server/architecture/contract_strategy.md`, `docs/domains/platform-mcp-server/architecture/api_contract.md`, `docs/domains/platform-mcp-server/delivery/platform_mcp_server_delivery.md`, `docs/domains/codex-hook-ingress/README.md`, `docs/delivery/coordination/**` | MCP-1 | готово | Стратегия контрактов готова: MCP-инструменты описываются через MCP SDK, JSON Schema и snapshot-проверки `tools/list`; Codex hooks вынесены в `codex-hook-ingress`; YAML-каталог не является каноникой. Код, proto и AsyncAPI не входят. |
| #760 | `services/internal/platform-mcp-server/**`, `services.yaml`, `docs/domains/platform-mcp-server/architecture/api_contract.md`, `docs/domains/platform-mcp-server/delivery/platform_mcp_server_delivery.md`, `docs/delivery/coordination/agent-1-project-catalog.md` | MCP-2 | готово | Сервисный каркас готов: процесс, конфигурация, health/readiness/metrics, MCP Streamable HTTP, `diagnostics.mcp_status.read`, каталог маршрутов к сервисам-владельцам и snapshot-проверка `tools/list`. Бизнес-маршруты, входной контур hooks, хранилище skills и манифесты выкладки не входят. |
| #771 | `services/internal/platform-mcp-server/**`, `services.yaml`, `docs/domains/platform-mcp-server/architecture/api_contract.md`, `docs/domains/platform-mcp-server/delivery/platform_mcp_server_delivery.md`, `docs/delivery/coordination/agent-1-project-catalog.md` | MCP-3 | готово | Подключены первые маршруты к `agent-manager`: старт сессии, старт `Run`, запись состояния `Run`, запись session snapshot и диагностика run context через безопасные сводки. Acceptance, follow-up и Human gate не регистрируются до готовности бизнес-реализации владельца. |
| #698 | `docs/platform/architecture/codex_hooks_and_skills.md`, `docs/domains/codex-hook-ingress/**` | hooks | решение выбрано, ждёт реализации | Hooks входят в MVP через hook emitter или локальный sidecar и отдельный `codex-hook-ingress`. Реализация hook emitter и входного контура выполняется отдельными срезами и не закрывается MCP-0/MCP-1. |
| #322 | `docs/domains/platform-mcp-server/README.md`, `docs/domains/platform-mcp-server/product/requirements.md`, `docs/domains/platform-mcp-server/architecture/design.md`, `docs/domains/platform-mcp-server/architecture/api_contract.md`, `docs/domains/platform-mcp-server/architecture/contract_strategy.md`, `docs/domains/platform-mcp-server/delivery/platform_mcp_server_delivery.md`, `docs/platform/architecture/mcp_and_interaction_model.md`, `docs/platform/architecture/codex_hooks_and_skills.md` | GOV-0/MCP boundary sync | active | Синхронизация MCP/hook маршрутов с `governance-manager`: risk/gate/release decisions идут в governance, `agent-manager` хранит ожидание flow, `interaction-hub` отвечает за delivery/callback. |
