---
doc_id: MAP-CK8S-DOMAIN-CODEX-HOOK-INGRESS
type: issue-map
title: kodex — карта Issue codex-hook-ingress
status: active
owner_role: KM
created_at: 2026-05-15
updated_at: 2026-05-22
---

# Карта Issue — codex-hook-ingress

## Кратко

Карта сервисного пакета `codex-hook-ingress`. Сервис принимает нормализованные Codex hook events от hook emitter или локального sidecar и не является MCP-сервером.

## Матрица

| Issue/PR | Документы | Срез | Статус | Примечание |
|---|---|---|---|---|
| #753 | `docs/domains/codex-hook-ingress/README.md`, `docs/domains/platform-mcp-server/architecture/contract_strategy.md`, `docs/platform/architecture/codex_hooks_and_skills.md`, `docs/platform/architecture/mcp_and_interaction_model.md`, `docs/platform/architecture/service_boundaries.md`, `docs/delivery/coordination/**` | MCP-1 | готово | Зафиксировано разделение MCP-сервера и hook ingress. Код, proto, OpenAPI и AsyncAPI не входят. |
| #698 | `docs/domains/codex-hook-ingress/README.md`, `docs/domains/codex-hook-ingress/product/requirements.md`, `docs/domains/codex-hook-ingress/architecture/design.md`, `docs/domains/codex-hook-ingress/architecture/data_model.md`, `docs/domains/codex-hook-ingress/architecture/api_contract.md`, `docs/domains/codex-hook-ingress/delivery/codex_hook_ingress_delivery.md`, `docs/platform/architecture/codex_hooks_and_skills.md` | CHI-0 | docs-first пакет подготовлен, реализация запланирована | Зафиксированы MVP hook events, границы с MCP, очистка входа, лимиты размера, routing владельцам и поддержка Codex skills как capability layer без skill-хранилища в ingress. |
| #322 | `docs/domains/platform-mcp-server/architecture/contract_strategy.md`, `docs/platform/architecture/codex_hooks_and_skills.md`, `docs/platform/architecture/mcp_and_interaction_model.md` | GOV-0/hook boundary sync | active | `PermissionRequest` и policy gate маршрутизируются в `governance-manager`; `agent-manager` хранит ожидание flow, `interaction-hub` доставляет запрос и callback. |
