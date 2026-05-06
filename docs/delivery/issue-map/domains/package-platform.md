---
doc_id: MAP-CK8S-DOMAIN-PACKAGE-PLATFORM
type: issue-map
title: kodex — карта Issue домена пакетной платформы
status: active
owner_role: KM
created_at: 2026-04-25
updated_at: 2026-05-06
---

# Карта Issue — пакетная платформа

## TL;DR

Долгоживущая карта домена `package-platform`.

## Матрица

| Issue/PR | Документы | Волна | Статус | Примечание |
|---|---|---|---|---|
| #642 | `docs/domains/package-platform/product/requirements.md`, `docs/domains/package-platform/architecture/design.md`, `docs/domains/package-platform/architecture/data_model.md`, `docs/domains/package-platform/architecture/api_contract.md`, `docs/domains/package-platform/delivery/package_hub.md`, `docs/catalogs/package-store/`, `docs/catalogs/plugins/`, `docs/catalogs/guidance-packages/` | PKG-1 | готово | Стартовый доменный пакет документации: границы, сущности, сценарии, события, риски и план поставки. |
| #646 | `proto/kodex/packages/**`, `specs/asyncapi/package-hub.v1.yaml`, `libs/go/platformevents/packagehub/**`, `libs/go/accesscatalog/**`, `docs/domains/package-platform/architecture/api_contract.md` | PKG-2 | готово к проверке | Контракты `package-hub`, события и действия доступа. |
| не назначено | `services/internal/package-hub/**`, `docs/domains/package-platform/architecture/data_model.md` | PKG-3 | запланировано | Сервисный каркас, БД, миграции, outbox и базовые чтения. |
| не назначено | `services/internal/package-hub/**`, `docs/catalogs/package-store/**` | PKG-4 | запланировано | Источники магазинов и синхронизация доступного каталога. |
| не назначено | `services/internal/package-hub/**`, `docs/catalogs/plugins/**`, `docs/catalogs/guidance-packages/**` | PKG-5/PKG-6 | запланировано | Установки, версии, секреты, плагины и руководящие пакеты. |
| не назначено | `deploy/**`, `services.yaml`, `docs/domains/package-platform/**` | PKG-7 | запланировано | Эксплуатационный контур `package-hub`: манифесты, migration job, config, health, metrics и runbook. |
