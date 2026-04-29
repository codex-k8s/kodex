---
doc_id: MAP-CK8S-DOMAIN-ACCESS-AND-ACCOUNTS
type: issue-map
title: kodex — карта Issue домена доступа и аккаунтов
status: active
owner_role: KM
created_at: 2026-04-25
updated_at: 2026-04-30
---

# Карта Issue — доступ, организации, группы и внешние аккаунты

## TL;DR

Долгоживущая карта домена `access-and-accounts`.

## Матрица

| Issue/PR | Документы | Волна | Статус | Примечание |
|---|---|---|---|---|
| #599 | `docs/domains/access-and-accounts/product/requirements.md`, `docs/domains/access-and-accounts/architecture/design.md`, `docs/domains/access-and-accounts/architecture/data_model.md`, `services/internal/access-manager/**`, `libs/go/postgres/**` | волна 7 | репозиторный срез готов | Добавлены доменные типы, миграция, контур членства, транзакционный PostgreSQL repository и базовые проверки SQL. |
| #600 | `docs/domains/access-and-accounts/product/requirements.md`, `docs/domains/access-and-accounts/architecture/data_model.md`, `docs/domains/access-and-accounts/architecture/api_contract.md`, `services/internal/access-manager/**` | волна 7 | репозиторный срез готов | Первичная инициализация пользователя по идентичности через allowlist получила путь записи и чтения PostgreSQL. |
| #601 | `docs/domains/access-and-accounts/product/requirements.md`, `docs/domains/access-and-accounts/architecture/design.md`, `docs/domains/access-and-accounts/architecture/data_model.md`, `docs/domains/access-and-accounts/architecture/api_contract.md`, `services/internal/access-manager/**` | волна 7 | репозиторный срез готов | Поставщики, внешние аккаунты, привязки и метаданные ссылки на секрет получили путь записи и чтения PostgreSQL. |
| #602 | `docs/domains/access-and-accounts/product/requirements.md`, `docs/domains/access-and-accounts/architecture/design.md`, `docs/domains/access-and-accounts/architecture/data_model.md`, `docs/domains/access-and-accounts/architecture/api_contract.md`, `services/internal/access-manager/**` | волна 7 | репозиторный срез готов | Каталог действий, правила доступа, явный запрет, аудит решений и outbox получили путь записи и чтения PostgreSQL. |
