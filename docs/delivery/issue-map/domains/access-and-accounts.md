---
doc_id: MAP-CK8S-DOMAIN-ACCESS-AND-ACCOUNTS
type: issue-map
title: kodex — карта Issue домена доступа и аккаунтов
status: active
owner_role: KM
created_at: 2026-04-25
updated_at: 2026-04-27
---

# Карта Issue — доступ, организации, группы и внешние аккаунты

## TL;DR

Долгоживущая карта домена `access-and-accounts`.

## Матрица

| Issue/PR | Документы | Волна | Статус | Примечание |
|---|---|---|---|---|
| #599 | `docs/domains/access-and-accounts/product/requirements.md`, `docs/domains/access-and-accounts/architecture/design.md`, `docs/domains/access-and-accounts/architecture/data_model.md`, `services/internal/access-manager/**` | волна 7 | реализация начата | Добавлены доменные типы, миграция, контур членства и тесты базового каркаса. |
| #600 | `docs/domains/access-and-accounts/product/requirements.md`, `docs/domains/access-and-accounts/architecture/data_model.md`, `docs/domains/access-and-accounts/architecture/api_contract.md`, `services/internal/access-manager/**` | волна 7 | реализация начата | Добавлена первичная инициализация пользователя по идентичности через allowlist как доменный сценарий. |
| #601 | `docs/domains/access-and-accounts/product/requirements.md`, `docs/domains/access-and-accounts/architecture/design.md`, `docs/domains/access-and-accounts/architecture/data_model.md`, `docs/domains/access-and-accounts/architecture/api_contract.md`, `services/internal/access-manager/**` | волна 7 | реализация начата | Добавлены поставщики, внешние аккаунты, привязки и метаданные ссылки на секрет. |
| #602 | `docs/domains/access-and-accounts/product/requirements.md`, `docs/domains/access-and-accounts/architecture/design.md`, `docs/domains/access-and-accounts/architecture/data_model.md`, `docs/domains/access-and-accounts/architecture/api_contract.md`, `services/internal/access-manager/**` | волна 7 | реализация начата | Добавлены каталог действий, правила доступа, явный запрет и аудит решений. |
