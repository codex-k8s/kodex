---
doc_id: MAP-CK8S-DOMAIN-PLATFORM-MCP-SERVER
type: issue-map
title: kodex — карта Issue platform-mcp-server
status: active
owner_role: KM
created_at: 2026-05-14
updated_at: 2026-05-14
---

# Карта Issue — platform-mcp-server

## TL;DR

Долгоживущая карта сервисного пакета `platform-mcp-server`. Это пограничный MCP-компонент, а не домен-владелец бизнес-состояния.

## Матрица

| Issue/PR | Документы | Срез | Статус | Примечание |
|---|---|---|---|---|
| #747 | `docs/domains/platform-mcp-server/product/requirements.md`, `docs/domains/platform-mcp-server/architecture/design.md`, `docs/domains/platform-mcp-server/architecture/api_contract.md`, `docs/domains/platform-mcp-server/delivery/platform_mcp_server_delivery.md`, `docs/platform/architecture/mcp_and_interaction_model.md`, `docs/platform/architecture/service_boundaries.md`, `docs/platform/architecture/codex_hooks_and_skills.md`, `docs/delivery/coordination/**` | MCP-0 | готово | Границы `platform-mcp-server`, ответственность, MVP-группы инструментов, безопасность и delivery-план. Код, proto и AsyncAPI не входят. |
| #753 | `docs/domains/platform-mcp-server/catalog/README.md`, `docs/domains/platform-mcp-server/catalog/tool_catalog.v1.yaml`, `docs/domains/platform-mcp-server/catalog/fixtures/**`, `docs/domains/platform-mcp-server/architecture/api_contract.md`, `docs/domains/platform-mcp-server/delivery/platform_mcp_server_delivery.md`, `docs/delivery/coordination/**` | MCP-1 | готово | Машинно-читаемый каталог инструментов, envelope, версии контрактов, Codex hooks без `PreCompact`/`PostCompact`, внутренние session events и тестовые примеры. Код, proto и AsyncAPI не входят. |
| #698 | `docs/platform/architecture/codex_hooks_and_skills.md`, `docs/domains/platform-mcp-server/**` | hooks | решение выбрано, ждёт реализации | Hooks входят в MVP через `platform-mcp-server` или локальный sidecar, но реализация hook emitter и ingress выполняется отдельными срезами и не закрывается MCP-0. |
