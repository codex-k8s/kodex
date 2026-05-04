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
| #599 | `docs/domains/access-and-accounts/product/requirements.md`, `docs/domains/access-and-accounts/architecture/design.md`, `docs/domains/access-and-accounts/architecture/data_model.md`, `services/internal/access-manager/**`, `libs/go/postgres/**` | волна 7 | gRPC-срез готов для реализованных операций | Добавлены доменные типы, миграции, подключение БД сервиса, контур членства, транзакционный PostgreSQL repository, gRPC-обработчики для организаций, групп и членства. |
| #600, #619 | `docs/domains/access-and-accounts/product/requirements.md`, `docs/domains/access-and-accounts/architecture/data_model.md`, `docs/domains/access-and-accounts/architecture/api_contract.md`, `services/internal/access-manager/**` | волна 7 | операторский жизненный цикл входа готов | Первичная инициализация пользователя по идентичности через allowlist получила путь записи и чтения PostgreSQL; gRPC-обработчики подключены для первичной инициализации пользователя, записи allowlist, изменения статуса пользователя, отключения allowlist-записи и списка pending/blocked. Операторские действия учитывают организационную область, а список pending/blocked для организации видит пользователя через allowlist до создания membership и сортируется по последнему изменению состояния. |
| #601 | `docs/domains/access-and-accounts/product/requirements.md`, `docs/domains/access-and-accounts/architecture/design.md`, `docs/domains/access-and-accounts/architecture/data_model.md`, `docs/domains/access-and-accounts/architecture/api_contract.md`, `services/internal/access-manager/**` | волна 7 | жизненный цикл внешних аккаунтов готов | Поставщики, внешние аккаунты, привязки и метаданные ссылки на секрет получили путь записи и чтения PostgreSQL; gRPC-обработчики подключены для создания, привязки, разрешения использования, обновления поставщика, изменения статуса аккаунта и отключения привязки. |
| #602 | `docs/domains/access-and-accounts/product/requirements.md`, `docs/domains/access-and-accounts/architecture/design.md`, `docs/domains/access-and-accounts/architecture/data_model.md`, `docs/domains/access-and-accounts/architecture/api_contract.md`, `services/internal/access-manager/**` | волна 7 | срез проверки доступа готов | Каталог действий, правила доступа, явный запрет, аудит решений и outbox получили путь записи и чтения PostgreSQL; gRPC-обработчики подключены для `PutAccessAction`, `PutAccessRule`, `CheckAccess` и `ExplainAccess`. |
| без отдельного Issue | `docs/domains/access-and-accounts/delivery/wave7_access_and_accounts.md`, `libs/go/grpcserver/**`, `services/internal/access-manager/**` | волна 7 | инфраструктурный срез готов | Общий gRPC runtime вынесен в `libs/go/grpcserver` до следующего доменного сервиса; доменная граница `access-manager` не смешана с общей библиотекой. |
