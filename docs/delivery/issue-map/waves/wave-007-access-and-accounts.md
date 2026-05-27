---
doc_id: MAP-CK8S-WAVE-007
type: issue-map
title: kodex — карта Issue волны 7
status: completed
owner_role: KM
created_at: 2026-04-25
updated_at: 2026-05-05
---

# Карта Issue — волна 7

## TL;DR

Волновая карта первого кодового домена: доступ, организации, группы и внешние аккаунты.

## Матрица

| Issue/PR | Документы | Домен | Статус | Примечание |
|---|---|---|---|---|
| #599 | `docs/domains/access-and-accounts/product/requirements.md`, `docs/domains/access-and-accounts/architecture/design.md`, `docs/domains/access-and-accounts/architecture/data_model.md`, `proto/kodex/access_accounts/v1/access_manager.proto`, `services/internal/access-manager/**`, `libs/go/postgres/**` | access-and-accounts | закрыта как выполненная | Организации, группы, членство и outbox получили PostgreSQL-репозиторий; gRPC-слой подключает `CreateOrganization`, `CreateGroup`, `SetMembership` и `ListMembershipGraph`. Операторское чтение графа членства проверяет `CommandMeta.actor` через `CheckAccess`. |
| #600 | `docs/domains/access-and-accounts/product/requirements.md`, `docs/domains/access-and-accounts/architecture/data_model.md`, `docs/domains/access-and-accounts/architecture/api_contract.md`, `proto/kodex/access_accounts/v1/access_manager.proto`, `services/internal/access-manager/**` | access-and-accounts | закрыта как выполненная | Путь первичной инициализации пользователя по allowlist получил PostgreSQL-записи пользователя, идентичности и правил допуска; gRPC-слой подключает `BootstrapUserFromIdentity`, `PutAllowlistEntry`, `SetUserStatus`, `DisableAllowlistEntry` и `ListPendingAccess`. |
| #601 | `docs/domains/access-and-accounts/product/requirements.md`, `docs/domains/access-and-accounts/architecture/design.md`, `docs/domains/access-and-accounts/architecture/data_model.md`, `docs/domains/access-and-accounts/architecture/api_contract.md`, `proto/kodex/access_accounts/v1/access_manager.proto`, `services/internal/access-manager/**` | access-and-accounts | закрыта как выполненная | Поставщики, внешние аккаунты, привязки и ссылки на секреты получили PostgreSQL-репозиторий; gRPC-слой подключает создание поставщика, регистрацию аккаунта, привязку, `ResolveExternalAccountUsage`, обновление поставщика, изменение статуса внешнего аккаунта и отключение привязки. |
| #602 | `docs/domains/access-and-accounts/product/requirements.md`, `docs/domains/access-and-accounts/architecture/design.md`, `docs/domains/access-and-accounts/architecture/data_model.md`, `docs/domains/access-and-accounts/architecture/api_contract.md`, `specs/asyncapi/access-manager.v1.yaml`, `services/internal/access-manager/**` | access-and-accounts | закрыта как выполненная | Каталог действий, правила доступа, аудит решений и outbox получили PostgreSQL repository; gRPC-слой подключает `PutAccessAction`, `PutAccessRule`, `CheckAccess` и `ExplainAccess`. Отключение правил остаётся административным хвостом будущих операторских сценариев. |
| без отдельного Issue | `docs/delivery/waves/wave-007-access-and-accounts.md`, `docs/domains/access-and-accounts/delivery/wave7_access_and_accounts.md`, `libs/go/grpcserver/**`, `services/internal/access-manager/**` | access-and-accounts | инфраструктурный срез готов | Общий gRPC-контур вынесен в `libs/go/grpcserver`; `access-manager` использует его как первый потребитель, а доменные обработчики и маппинг ошибок остаются в сервисе. |
| без отдельного Issue | `docs/platform/architecture/data_model.md`, `docs/domains/access-and-accounts/architecture/design.md`, `docs/domains/access-and-accounts/delivery/wave7_access_and_accounts.md`, `libs/go/eventlog/**`, `services/internal/access-manager/**`, `services/internal/platform-event-log/**`, `deploy/base/postgres/**`, `deploy/base/platform-event-log/migrations.yaml.tpl` | access-and-accounts | MVP-доставка событий готова | Добавлен отдельный контур `platform-event-log` с миграциями общего PostgreSQL-журнала, checkpoint API для потребителей, подготовка БД, секрет времени выполнения, промышленное задание миграций и штатный канал публикации `postgres-event-log` из сервисного outbox. Диагностический канал остаётся только для ручной диагностики. |
| без отдельного Issue | `services/internal/access-manager/Dockerfile`, `deploy/base/access-manager/**`, `deploy/base/postgres/**`, `bootstrap/host/config.env.example`, `bootstrap/host/bootstrap_cluster.sh`, `cmd/manifest-render/**`, `scripts/build-access-manager-images.sh`, `scripts/smoke-access-manager.sh`, `libs/go/grpcserver/**` | access-and-accounts | эксплуатационный срез готов | Добавлены образ миграций, Kubernetes-манифесты сервиса и миграций, секрет времени выполнения gRPC-token/DSN, рендер шаблонов выкладки, сборка проверочных образов, проверочный путь готовности и корреляционные gRPC-метаданные для логов. |
