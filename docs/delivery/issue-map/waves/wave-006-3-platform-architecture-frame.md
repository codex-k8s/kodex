---
doc_id: MAP-CK8S-WAVE-006-3
type: issue-map
title: kodex — карта Issue волны 6.3
status: active
owner_role: KM
created_at: 2026-04-26
updated_at: 2026-04-26
---

# Карта Issue — волна 6.3

## Кратко

Волновая карта документационной волны 6.3. После слияния остаётся историческим снимком сквозного архитектурного каркаса.

## Матрица

| Issue/PR | Документы | Домен | Статус | Примечание |
|---|---|---|---|---|
| PR текущей волны | `docs/platform/architecture/c4_context.md`, `docs/platform/architecture/c4_container.md`, `docs/platform/architecture/domain_map.md`, `docs/platform/architecture/service_boundaries.md`, `docs/platform/architecture/data_model.md`, `docs/platform/architecture/provider_integration_model.md`, `docs/platform/architecture/mcp_and_interaction_model.md` | Архитектура платформы | готово | Перенос сквозной архитектурной рамки из `refactoring/**` в активную структуру `docs/**`. |
| #599 | `docs/platform/architecture/domain_map.md`, `docs/platform/architecture/service_boundaries.md`, `docs/platform/architecture/data_model.md` | Доступ и аккаунты | запланировано | Организации, группы и граф членства должны лечь в сервис-владелец `access-manager`. |
| #600 | `docs/platform/architecture/domain_map.md`, `docs/platform/architecture/service_boundaries.md`, `docs/platform/architecture/data_model.md` | Доступ и аккаунты | запланировано | Вход пользователя, allowlist и SSO/OIDC относятся к сервису-владельцу `access-manager`. |
| #601 | `docs/platform/architecture/provider_integration_model.md`, `docs/platform/architecture/data_model.md` | Доступ и аккаунты | запланировано | Внешние аккаунты и операции провайдера должны иметь явный контур владения и область действия политики. |
| #602 | `docs/platform/architecture/service_boundaries.md`, `docs/platform/architecture/mcp_and_interaction_model.md` | Доступ и аккаунты | запланировано | Вычисление прав доступа должно проходить через контур владения, политику и аудит. |

## Правила

- Эта карта не заменяет доменную карту `docs/delivery/issue-map/domains/access-and-accounts.md`.
- Домен доступа должен ссылаться на архитектурные документы, а не копировать сквозные правила целиком.
