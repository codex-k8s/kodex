---
doc_id: MAP-CK8S-WAVE-007
type: issue-map
title: kodex — карта Issue волны 7
status: active
owner_role: KM
created_at: 2026-04-25
updated_at: 2026-04-30
---

# Карта Issue — волна 7

## TL;DR

Волновая карта первого кодового домена: доступ, организации, группы и внешние аккаунты.

## Матрица

| Issue/PR | Документы | Домен | Статус | Примечание |
|---|---|---|---|---|
| #599 | `docs/domains/access-and-accounts/product/requirements.md`, `docs/domains/access-and-accounts/architecture/design.md`, `docs/domains/access-and-accounts/architecture/data_model.md`, `proto/kodex/access_accounts/v1/access_manager.proto`, `services/internal/access-manager/**`, `libs/go/postgres/**` | access-and-accounts | репозиторный срез готов | Организации, группы, членство и outbox получили PostgreSQL repository для текущего доменного интерфейса; транспортные обработчики остаются в следующем срезе. |
| #600 | `docs/domains/access-and-accounts/product/requirements.md`, `docs/domains/access-and-accounts/architecture/data_model.md`, `docs/domains/access-and-accounts/architecture/api_contract.md`, `proto/kodex/access_accounts/v1/access_manager.proto`, `services/internal/access-manager/**` | access-and-accounts | репозиторный срез готов | Путь первичной инициализации пользователя по allowlist получил PostgreSQL-записи пользователя, идентичности и правил допуска; gRPC путь входа остаётся в бэклоге транспортного среза. |
| #601 | `docs/domains/access-and-accounts/product/requirements.md`, `docs/domains/access-and-accounts/architecture/design.md`, `docs/domains/access-and-accounts/architecture/data_model.md`, `docs/domains/access-and-accounts/architecture/api_contract.md`, `proto/kodex/access_accounts/v1/access_manager.proto`, `services/internal/access-manager/**` | access-and-accounts | репозиторный срез готов | Поставщики, внешние аккаунты, привязки и ссылки на секреты получили PostgreSQL repository; реальное взаимодействие с сервисами провайдеров остаётся за последующими срезами. |
| #602 | `docs/domains/access-and-accounts/product/requirements.md`, `docs/domains/access-and-accounts/architecture/design.md`, `docs/domains/access-and-accounts/architecture/data_model.md`, `docs/domains/access-and-accounts/architecture/api_contract.md`, `specs/asyncapi/access-manager.v1.yaml`, `services/internal/access-manager/**` | access-and-accounts | репозиторный срез готов | Каталог действий, правила доступа, аудит решений и outbox получили PostgreSQL repository; `ExplainAccess`, доставка событий и полный транспорт остаются в бэклоге задачи. |
