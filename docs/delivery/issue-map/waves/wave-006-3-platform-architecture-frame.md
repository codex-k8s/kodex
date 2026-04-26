---
doc_id: MAP-CK8S-WAVE-006-3
type: issue-map
title: kodex — карта Issue wave 6.3
status: active
owner_role: KM
created_at: 2026-04-26
updated_at: 2026-04-26
---

# Карта Issue — wave 6.3

## TL;DR

Волновая карта документационной wave 6.3. После merge остаётся историческим снимком сквозного архитектурного каркаса.

## Матрица

| Issue/PR | Документы | Домен | Статус | Примечание |
|---|---|---|---|---|
| PR текущей волны | `docs/platform/architecture/c4_context.md`, `docs/platform/architecture/c4_container.md`, `docs/platform/architecture/domain_map.md`, `docs/platform/architecture/service_boundaries.md`, `docs/platform/architecture/data_model.md`, `docs/platform/architecture/provider_integration_model.md`, `docs/platform/architecture/mcp_and_interaction_model.md` | platform architecture | ready | Перенос сквозной архитектурной рамки из `refactoring/**` в активную структуру `docs/**`. |
| #599 | `docs/platform/architecture/domain_map.md`, `docs/platform/architecture/service_boundaries.md`, `docs/platform/architecture/data_model.md` | access-and-accounts | planned | Организации, группы и граф членства должны лечь в owner-сервис `access-manager`. |
| #600 | `docs/platform/architecture/domain_map.md`, `docs/platform/architecture/service_boundaries.md`, `docs/platform/architecture/data_model.md` | access-and-accounts | planned | Вход пользователя, allowlist и SSO/OIDC относятся к owner-сервису `access-manager`. |
| #601 | `docs/platform/architecture/provider_integration_model.md`, `docs/platform/architecture/data_model.md` | access-and-accounts | planned | Внешние аккаунты и provider accounts должны иметь явный owner-контур и policy scope. |
| #602 | `docs/platform/architecture/service_boundaries.md`, `docs/platform/architecture/mcp_and_interaction_model.md` | access-and-accounts | planned | Вычисление прав доступа должно проходить через owner-контур, policy и audit. |

## Правила

- Эта карта не заменяет доменную карту `docs/delivery/issue-map/domains/access-and-accounts.md`.
- Домен доступа должен ссылаться на архитектурные документы, а не копировать сквозные правила целиком.
