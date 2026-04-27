---
doc_id: MAP-CK8S-WAVE-007
type: issue-map
title: kodex — карта Issue волны 7
status: active
owner_role: KM
created_at: 2026-04-25
updated_at: 2026-04-27
---

# Карта Issue — волна 7

## TL;DR

Волновая карта первого кодового домена: доступ, организации, группы и внешние аккаунты.

## Матрица

| Issue/PR | Документы | Домен | Статус | Примечание |
|---|---|---|---|---|
| #599 | `docs/domains/access-and-accounts/product/requirements.md`, `docs/domains/access-and-accounts/architecture/design.md`, `docs/domains/access-and-accounts/architecture/data_model.md`, `proto/kodex/access_accounts/v1/access_manager.proto`, `services/internal/access-manager/**` | access-and-accounts | контракт готов, реализация начата | Базовая модель организаций, групп и членства добавлена в новый сервис; транспортные обработчики и полный репозиторий остаются в бэклоге задачи. |
| #600 | `docs/domains/access-and-accounts/product/requirements.md`, `docs/domains/access-and-accounts/architecture/data_model.md`, `docs/domains/access-and-accounts/architecture/api_contract.md`, `specs/openapi/access-manager.v1.yaml`, `services/internal/access-manager/**` | access-and-accounts | контракт готов, реализация начата | Добавлена доменная первичная инициализация пользователя по allowlist; HTTP/gRPC путь входа остаётся в бэклоге задачи. |
| #601 | `docs/domains/access-and-accounts/product/requirements.md`, `docs/domains/access-and-accounts/architecture/design.md`, `docs/domains/access-and-accounts/architecture/data_model.md`, `docs/domains/access-and-accounts/architecture/api_contract.md`, `specs/openapi/access-manager.v1.yaml`, `services/internal/access-manager/**` | access-and-accounts | контракт готов, реализация начата | Добавлен базовый контур внешних аккаунтов и ссылок на секреты; полный жизненный цикл и транспорт остаются в бэклоге задачи. |
| #602 | `docs/domains/access-and-accounts/product/requirements.md`, `docs/domains/access-and-accounts/architecture/design.md`, `docs/domains/access-and-accounts/architecture/data_model.md`, `docs/domains/access-and-accounts/architecture/api_contract.md`, `specs/asyncapi/access-manager.v1.yaml`, `services/internal/access-manager/**` | access-and-accounts | контракт готов, реализация начата | Добавлен базовый контур вычисления доступа, явного запрета и аудита; `ExplainAccess`, списки и доставка событий остаются в бэклоге задачи. |
