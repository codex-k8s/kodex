---
doc_id: MAP-CK8S-DOMAIN-CODEX-HOOK-INGRESS
type: issue-map
title: kodex — карта Issue codex-hook-ingress
status: active
owner_role: KM
created_at: 2026-05-15
updated_at: 2026-05-26
---

# Карта Issue — codex-hook-ingress

## Кратко

Карта сервисного пакета `codex-hook-ingress`. Сервис принимает нормализованные Codex hook events от hook emitter или локального sidecar и не является MCP-сервером.

## Матрица

| Issue/PR | Документы | Срез | Статус | Примечание |
|---|---|---|---|---|
| #753 | `docs/domains/codex-hook-ingress/README.md`, `docs/domains/platform-mcp-server/architecture/contract_strategy.md`, `docs/platform/architecture/codex_hooks_and_skills.md`, `docs/platform/architecture/mcp_and_interaction_model.md`, `docs/platform/architecture/service_boundaries.md`, `docs/delivery/coordination/**` | MCP-1 | готово | Зафиксировано разделение MCP-сервера и hook ingress. Код, proto, OpenAPI и AsyncAPI не входят. |
| #698 | `docs/domains/codex-hook-ingress/README.md`, `docs/domains/codex-hook-ingress/product/requirements.md`, `docs/domains/codex-hook-ingress/architecture/design.md`, `docs/domains/codex-hook-ingress/architecture/data_model.md`, `docs/domains/codex-hook-ingress/architecture/api_contract.md`, `docs/domains/codex-hook-ingress/delivery/codex_hook_ingress_delivery.md`, `docs/platform/architecture/codex_hooks_and_skills.md` | CHI-0 | docs-first пакет подготовлен, реализация запланирована | Зафиксированы MVP hook events, границы с MCP, очистка входа, лимиты размера, routing владельцам и поддержка Codex skills как capability layer без skill-хранилища в ingress. |
| #778 | `specs/jsonschema/codex-hook-ingress.v1/**`, `specs/README.md`, `docs/domains/codex-hook-ingress/**`, `docs/platform/architecture/codex_hooks_and_skills.md`, `docs/platform/architecture/mcp_and_interaction_model.md`, `docs/domains/platform-mcp-server/architecture/contract_strategy.md`, `docs/delivery/coordination/**` | CHI-1 | machine-readable схемы подготовлены | Зафиксированы JSON Schema normalized hook envelope и sanitizer contract, safe examples, validation command, downstream safe parts и явное отделение hook envelope от MCP tools и business commands. |
| #786 | `docs/domains/codex-hook-ingress/architecture/emitter_sidecar_contract.md`, `specs/jsonschema/codex-hook-ingress.v1/hook-emitter-config.v1.schema.json`, `specs/jsonschema/codex-hook-ingress.v1/hook-emitter-config.defaults.json`, `docs/domains/codex-hook-ingress/**`, `docs/platform/architecture/codex_hooks_and_skills.md`, `docs/platform/architecture/mcp_and_interaction_model.md`, `docs/platform/architecture/service_boundaries.md`, `docs/domains/platform-mcp-server/architecture/contract_strategy.md`, `docs/delivery/coordination/agent-5-codex-hook-ingress.md` | CHI-2 | runtime contract подготовлен | Зафиксированы роль hook emitter/local sidecar, logical `SubmitHookEvent` в `codex-hook-ingress`, sanitizer до buffer/send, auth, idempotency, ordering, retry, bounded buffer, backpressure и failure policy без выбора physical transport. |
| #793 | `services/internal/codex-hook-ingress/**`, `services.yaml`, `docs/domains/codex-hook-ingress/**`, `docs/delivery/coordination/agent-5-codex-hook-ingress.md`, `docs/delivery/issue-map/domains/codex-hook-ingress.md` | CHI-3 | сервисный каркас подготовлен | Добавлен runnable service skeleton с health/readiness/metrics и in-process logical `SubmitHookEvent`; физический transport и маршруты к соседним доменам не выбраны и не реализованы. |
| #808 | `services/internal/codex-hook-ingress/**`, `services.yaml`, `docs/domains/codex-hook-ingress/**`, `docs/delivery/coordination/agent-5-codex-hook-ingress.md`, `docs/delivery/issue-map/domains/codex-hook-ingress.md` | CHI-4 | route registry подготовлен | Добавлены owner ports/stubs и dispatch только safe event parts; disabled/unsupported/downstream-failed routes возвращают safe diagnostics и не считаются успешной доставкой. |
| #823 | `services/internal/codex-hook-ingress/**`, `services.yaml`, `docs/domains/codex-hook-ingress/**`, `docs/delivery/coordination/agent-5-codex-hook-ingress.md`, `docs/delivery/issue-map/domains/codex-hook-ingress.md` | CHI-6a | ops feed и diagnostics подготовлены | Добавлены bounded in-memory ops/realtime feed, TTL/capacity retention, sanitizer metrics, route diagnostics, fixed-window rate limits и safe backpressure без служебной БД и без raw payload storage. |
| #322 | `docs/domains/platform-mcp-server/architecture/contract_strategy.md`, `docs/platform/architecture/codex_hooks_and_skills.md`, `docs/platform/architecture/mcp_and_interaction_model.md` | GOV-0/hook boundary sync | active | `PermissionRequest` и policy gate маршрутизируются в `governance-manager`; `agent-manager` хранит ожидание flow, `interaction-hub` доставляет запрос и callback. |
