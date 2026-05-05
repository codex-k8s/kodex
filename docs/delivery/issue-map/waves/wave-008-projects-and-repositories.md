---
doc_id: MAP-CK8S-WAVE-008
type: issue-map
title: kodex — карта Issue волны 8
status: active
owner_role: KM
created_at: 2026-05-05
updated_at: 2026-05-05
---

# Карта Issue — волна 8

## TL;DR

Волновая карта домена проектов и репозиториев.

## Матрица

| Issue/PR | Документы | Домен | Статус | Примечание |
|---|---|---|---|---|
| #628 | `docs/domains/projects-and-repositories/**`, `docs/delivery/waves/wave-008-projects-and-repositories.md`, `docs/delivery/issue-map/domains/projects-and-repositories.md` | projects-and-repositories | закрывается как выполненная | Стартовый срез фиксирует доменный пакет, план поставки и очередь малых PR-срезов. |
| #629 | `docs/domains/projects-and-repositories/architecture/api_contract.md`, `proto/kodex/projects/v1/project_catalog.proto`, `specs/asyncapi/project-catalog.v1.yaml`, `services/internal/project-catalog/**` | projects-and-repositories | запланирована | Контракты и сервисный каркас. |
| #630 | `docs/domains/projects-and-repositories/architecture/data_model.md`, `services/internal/project-catalog/**`, `libs/go/postgres/**` | projects-and-repositories | запланирована | PostgreSQL-модель, миграции, слой репозитория, outbox и тесты. |
| #631 | `docs/domains/projects-and-repositories/architecture/api_contract.md`, `services/internal/project-catalog/**`, `libs/go/grpcserver/**` | projects-and-repositories | запланирована | gRPC-операции, граница проверки доступа, доменные события и тесты транспорта. |
| #632 | `docs/domains/projects-and-repositories/product/requirements.md`, `docs/domains/projects-and-repositories/architecture/design.md`, `docs/domains/projects-and-repositories/architecture/data_model.md` | projects-and-repositories | запланирована | Политика `services.yaml`, источники документации и политика рабочего контура. |
| #633 | `docs/domains/projects-and-repositories/delivery/wave8_project_catalog.md`, `deploy/base/project-catalog/**`, `services/internal/project-catalog/**` | projects-and-repositories | запланирована | Правила веток, релизная политика, политика размещения, манифесты и закрывающий контрольный срез. |
| #281, #282 | `docs/domains/projects-and-repositories/**`, `docs/delivery/issue-map/domains/provider-native-work-items.md` | projects-and-repositories, provider-native-work-items | остаются открытыми | Wave 8 создаёт проектное основание подключения репозиториев; provider-native создание, сканирование и первичный PR требуют следующих срезов. |
