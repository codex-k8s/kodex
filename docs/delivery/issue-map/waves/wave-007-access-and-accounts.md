---
doc_id: MAP-CK8S-WAVE-007
type: issue-map
title: kodex — карта Issue волны 7
status: active
owner_role: KM
created_at: 2026-04-25
updated_at: 2026-05-04
---

# Карта Issue — волна 7

## TL;DR

Волновая карта первого кодового домена: доступ, организации, группы и внешние аккаунты.

## Матрица

| Issue/PR | Документы | Домен | Статус | Примечание |
|---|---|---|---|---|
| #599 | `docs/domains/access-and-accounts/product/requirements.md`, `docs/domains/access-and-accounts/architecture/design.md`, `docs/domains/access-and-accounts/architecture/data_model.md`, `proto/kodex/access_accounts/v1/access_manager.proto`, `services/internal/access-manager/**`, `libs/go/postgres/**` | access-and-accounts | gRPC-срез готов для реализованных операций | Организации, группы, членство и outbox получили PostgreSQL repository; gRPC-слой регистрирует `AccessManagerService` и подключает обработчики `CreateOrganization`, `CreateGroup`, `SetMembership` к доменному сервису. |
| #600 | `docs/domains/access-and-accounts/product/requirements.md`, `docs/domains/access-and-accounts/architecture/data_model.md`, `docs/domains/access-and-accounts/architecture/api_contract.md`, `proto/kodex/access_accounts/v1/access_manager.proto`, `services/internal/access-manager/**` | access-and-accounts | операторский жизненный цикл входа готов | Путь первичной инициализации пользователя по allowlist получил PostgreSQL-записи пользователя, идентичности и правил допуска; gRPC-слой подключает `BootstrapUserFromIdentity`, `PutAllowlistEntry`, `SetUserStatus`, `DisableAllowlistEntry` и `ListPendingAccess`. |
| #601 | `docs/domains/access-and-accounts/product/requirements.md`, `docs/domains/access-and-accounts/architecture/design.md`, `docs/domains/access-and-accounts/architecture/data_model.md`, `docs/domains/access-and-accounts/architecture/api_contract.md`, `proto/kodex/access_accounts/v1/access_manager.proto`, `services/internal/access-manager/**` | access-and-accounts | жизненный цикл внешних аккаунтов готов | Поставщики, внешние аккаунты, привязки и ссылки на секреты получили PostgreSQL-репозиторий; gRPC-слой подключает создание поставщика, регистрацию аккаунта, привязку, `ResolveExternalAccountUsage`, обновление поставщика, изменение статуса внешнего аккаунта и отключение привязки. |
| #602 | `docs/domains/access-and-accounts/product/requirements.md`, `docs/domains/access-and-accounts/architecture/design.md`, `docs/domains/access-and-accounts/architecture/data_model.md`, `docs/domains/access-and-accounts/architecture/api_contract.md`, `specs/asyncapi/access-manager.v1.yaml`, `services/internal/access-manager/**` | access-and-accounts | срез проверки доступа готов | Каталог действий, правила доступа, аудит решений и outbox получили PostgreSQL repository; gRPC-слой подключает `PutAccessAction`, `PutAccessRule`, `CheckAccess` и `ExplainAccess`, а отключение правил, доставка событий и операторские представления аудита остаются в бэклоге. |
| без отдельного Issue | `docs/delivery/waves/wave-007-access-and-accounts.md`, `docs/domains/access-and-accounts/delivery/wave7_access_and_accounts.md`, `libs/go/grpcserver/**`, `services/internal/access-manager/**` | access-and-accounts | инфраструктурный срез готов | Общий gRPC runtime вынесен в `libs/go/grpcserver`; `access-manager` использует его как первый потребитель, а доменные обработчики и маппинг ошибок остаются в сервисе. |
| без отдельного Issue | `docs/platform/architecture/data_model.md`, `docs/domains/access-and-accounts/architecture/design.md`, `docs/domains/access-and-accounts/delivery/wave7_access_and_accounts.md`, `libs/go/eventlog/**`, `services/internal/access-manager/**`, `services/internal/platform-event-log/**`, `deploy/base/postgres/**`, `deploy/base/platform-event-log/migrations.yaml.tpl` | access-and-accounts | MVP-доставка событий готова | Добавлен отдельный контур `platform-event-log` с миграциями общего PostgreSQL-журнала, checkpoint API для потребителей, подготовка БД, runtime-секрет, production-задание миграций и штатный канал публикации `postgres-event-log` из сервисного outbox. Диагностический канал остаётся только для ручной диагностики. |
