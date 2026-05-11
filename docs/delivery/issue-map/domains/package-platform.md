---
doc_id: MAP-CK8S-DOMAIN-PACKAGE-PLATFORM
type: issue-map
title: kodex — карта Issue домена пакетной платформы
status: active
owner_role: KM
created_at: 2026-04-25
updated_at: 2026-05-11
---

# Карта Issue — пакетная платформа

## TL;DR

Долгоживущая карта домена `package-platform`.

## Матрица

| Issue/PR | Документы | Волна | Статус | Примечание |
|---|---|---|---|---|
| #642 | `docs/domains/package-platform/product/requirements.md`, `docs/domains/package-platform/architecture/design.md`, `docs/domains/package-platform/architecture/data_model.md`, `docs/domains/package-platform/architecture/api_contract.md`, `docs/domains/package-platform/delivery/package_hub.md`, `docs/catalogs/package-store/`, `docs/catalogs/plugins/`, `docs/catalogs/guidance-packages/` | PKG-1 | готово | Стартовый доменный пакет документации: границы, сущности, сценарии, события, риски и план поставки. |
| #646 | `proto/kodex/packages/**`, `specs/asyncapi/package-hub.v1.yaml`, `libs/go/platformevents/packagehub/**`, `libs/go/accesscatalog/**`, `docs/domains/package-platform/architecture/api_contract.md` | PKG-2 | готово | Контракты `package-hub`, события и действия доступа. |
| #650 | `services/internal/package-hub/**`, `docs/domains/package-platform/delivery/package_hub.md`, `docs/domains/package-platform/architecture/api_contract.md` | PKG-3.1 | готово | Минимальный каркас процесса `package-hub`: gRPC runtime, health, metrics и stub-операции. |
| #663 | `libs/go/grpcserver/**`, `docs/design-guidelines/common/external_dependencies_catalog.md`, `docs/domains/package-platform/delivery/package_hub.md` | runtime перед PKG-3.2 | готово | Общий gRPC runtime создаёт OpenTelemetry span для входящих RPC и извлекает W3C trace context для новых сервисов. |
| #667 | `services/internal/package-hub/cmd/cli/migrations/**`, `services/internal/package-hub/internal/domain/**`, `services/internal/package-hub/internal/repository/postgres/catalog/**`, `scripts/test-go-postgres.sh`, `docs/domains/package-platform/delivery/package_hub.md` | PKG-3.2 | готово | PostgreSQL-модель каталога пакетов: источники, пакеты, версии, снимки manifest и ценовые метаданные без gRPC business handlers. |
| #670 | `services/internal/package-hub/cmd/cli/migrations/**`, `services/internal/package-hub/internal/domain/**`, `services/internal/package-hub/internal/repository/postgres/catalog/**`, `docs/domains/package-platform/delivery/package_hub.md` | PKG-3.3 | готово | PostgreSQL-модель установок, схем секретов, проверок и command result без gRPC business handlers и outbox. |
| #673 | `services/internal/package-hub/**`, `docs/domains/package-platform/architecture/data_model.md`, `docs/domains/package-platform/delivery/package_hub.md` | PKG-3.4 | готово | Outbox, базовые repository-backed gRPC чтения и команда проверки версии пакета. |
| #678 | `services/internal/package-hub/**`, `docs/domains/package-platform/architecture/api_contract.md`, `docs/domains/package-platform/architecture/design.md`, `docs/domains/package-platform/delivery/package_hub.md` | PKG-4.1 | готово | Команды жизненного цикла источников пакетов: подключение, обновление, отключение, идемпотентность и события. |
| #680 | `proto/kodex/packages/**`, `proto/gen/go/kodex/packages/**`, `services/internal/package-hub/**`, `docs/domains/package-platform/architecture/api_contract.md`, `docs/domains/package-platform/architecture/design.md`, `docs/domains/package-platform/delivery/package_hub.md` | PKG-4.2 | готово | Синхронизация доступного каталога принимает нормализованный снимок, проверяет manifest, создаёт или обновляет packages/versions и пишет события. |
| #684 | `services/internal/package-hub/**`, `docs/domains/package-platform/architecture/api_contract.md`, `docs/domains/package-platform/architecture/design.md`, `docs/domains/package-platform/delivery/package_hub.md` | PKG-5.1 | готово | Запрос установки пакета и чтения установок: проверка доступа, идемпотентность, manifest-derived статусы и события установки. |
| #689 | `services/internal/package-hub/**`, `docs/domains/package-platform/architecture/api_contract.md`, `docs/domains/package-platform/architecture/data_model.md`, `docs/domains/package-platform/architecture/design.md`, `docs/domains/package-platform/delivery/package_hub.md` | PKG-5.2 | готово | Изменение, отключение и снятие установок: expected version, command result, outbox и gRPC handlers. |
| #692 | `services/internal/package-hub/**`, `docs/domains/package-platform/architecture/api_contract.md`, `docs/domains/package-platform/architecture/data_model.md`, `docs/domains/package-platform/architecture/design.md`, `docs/domains/package-platform/delivery/package_hub.md` | PKG-5.3a | готово | Чтение схем секретов: снимки схем из manifest при синхронизации каталога, событие `package.secret_schema.updated` и gRPC `GetPackageSecretSchema`. |
| не назначено | `services/internal/package-hub/**`, `docs/domains/package-platform/**` | PKG-5.3b | запланировано | Сверка статуса заполненности секретов установки после согласования контракта заполненности секретов пакета в `access-manager`. |
| не назначено | `services/internal/package-hub/**`, `docs/catalogs/plugins/**`, `docs/catalogs/guidance-packages/**` | PKG-6 | запланировано | Плагины, руководящие пакеты, магазин и пакеты пользовательского контента платформы. |
| не назначено | `deploy/**`, `services.yaml`, `docs/domains/package-platform/**` | PKG-7 | запланировано | Эксплуатационный контур `package-hub`: манифесты, migration job, config, health, metrics и runbook. |
