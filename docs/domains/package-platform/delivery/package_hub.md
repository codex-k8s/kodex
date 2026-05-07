---
doc_id: DLV-CK8S-PACKAGE-HUB
type: delivery-plan
title: kodex — поставка package-hub
status: active
owner_role: EM
created_at: 2026-05-06
updated_at: 2026-05-07
related_issues: [642, 646, 650, 663, 667, 670, 673, 678]
related_prs: []
related_docsets:
  - docs/domains/package-platform/product/requirements.md
  - docs/domains/package-platform/architecture/design.md
  - docs/domains/package-platform/architecture/data_model.md
  - docs/domains/package-platform/architecture/api_contract.md
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-06-package-platform-kickoff"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-06
---

# Поставка package-hub

## TL;DR

`package-hub` поставляется малыми PR-срезами: сначала доменный пакет документации, затем контракты, сервисный каркас, PostgreSQL-модель, источники магазинов, установки пакетов, специализация видов пакетов и эксплуатационный контур.

## Входные артефакты

| Документ | Путь |
|---|---|
| Требования домена | `docs/domains/package-platform/product/requirements.md` |
| Дизайн домена | `docs/domains/package-platform/architecture/design.md` |
| Модель данных | `docs/domains/package-platform/architecture/data_model.md` |
| API-обзор | `docs/domains/package-platform/architecture/api_contract.md` |
| Карта Issue | `docs/delivery/issue-map/domains/package-platform.md` |

## Срезы поставки

| Срез | Issue | Результат |
|---|---|---|
| PKG-1 | #642 | Доменная документация, план поставки, каталоги и карта связей готовы. |
| PKG-2 | #646 | gRPC и AsyncAPI контракты `package-hub`, действия доступа и список событий готовы. |
| Runtime перед PKG-3.2 | #663 | Общий `libs/go/grpcserver` подключает OpenTelemetry `StatsHandler`, создаёт серверный span для входящих RPC и извлекает W3C `traceparent`/`baggage`; экспортёр и OTel Collector боевого контура подключаются отдельным срезом начальной настройки. |
| PKG-3.1 | #650 | Сервисный процесс, служебный HTTP-контур, общий gRPC runtime и регистрация `PackageHubService` готовы без БД-логики. |
| PKG-3.2 | #667 | PostgreSQL-модель источников, пакетов, версий, manifest и ценовых метаданных готова: миграции, доменные типы, repository-контракт, PostgreSQL repository и интеграционные тесты. gRPC business handlers не входят в этот срез. |
| PKG-3.3 | #670 | PostgreSQL-модель установок, схем секретов, проверок, идемпотентности и optimistic concurrency готова: миграции, доменные типы, repository-контракт, PostgreSQL repository и интеграционные тесты. gRPC business handlers и outbox не входят в этот срез. |
| PKG-3.4 | #673 | Outbox, первые repository-backed gRPC чтения и команда проверки версии пакета готовы. |
| PKG-4.1 | #678 | Команды подключения, обновления и отключения источников пакетов готовы: проверка доступа, идемпотентность, ожидаемая версия и события источника. |
| PKG-4.2 | не назначено | Синхронизация доступного каталога и проверка manifest готовы. |
| PKG-5 | не назначено | Установки пакетов, версии, привязки секретов и события установки готовы. |
| PKG-6 | не назначено | Специализация плагинов, руководящих пакетов, магазина и пакетов пользовательского контента платформы готова. |
| PKG-7 | не назначено | Манифесты deploy, migration job, config, health, metrics и runbook готовы. |

## Статус операций `PackageHubService`

Стабильный `PackageHubService v1` уже фиксирует полный транспортный контракт домена. Реализация идёт малыми срезами: готовые операции ниже работают через доменный сервис и PostgreSQL repository, остальные пока возвращают `unimplemented` через сгенерированный `UnimplementedPackageHubServiceServer`.

| Операция | Текущий статус кода | Плановый срез |
|---|---|---|
| `ConnectPackageSource` | `ready` | PKG-4.1 |
| `UpdatePackageSource` | `ready` | PKG-4.1 |
| `DisablePackageSource` | `ready` | PKG-4.1 |
| `GetPackageSource` | `ready` | PKG-3.4 |
| `ListPackageSources` | `ready` | PKG-3.4 |
| `SyncAvailablePackages` | `unimplemented` | PKG-4.2 |
| `GetPackage` | `ready` | PKG-3.4 |
| `ListPackages` | `ready` | PKG-3.4 |
| `GetPackageVersion` | `ready` | PKG-3.4 |
| `ListPackageVersions` | `ready` | PKG-3.4 |
| `GetPackageManifest` | `ready` | PKG-3.4 |
| `RequestPackageInstallation` | `unimplemented` | PKG-5 |
| `UpdatePackageInstallation` | `unimplemented` | PKG-5 |
| `DisablePackageInstallation` | `unimplemented` | PKG-5 |
| `UninstallPackage` | `unimplemented` | PKG-5 |
| `GetPackageInstallation` | `unimplemented` | PKG-5 |
| `ListPackageInstallations` | `unimplemented` | PKG-5 |
| `GetPackageSecretSchema` | `unimplemented` | PKG-5 |
| `RefreshPackageInstallationSecretStatus` | `unimplemented` | PKG-5 |
| `SetPackageVerification` | `ready` | PKG-3.4 |

## Наблюдаемость `PKG-3.1`

| Область | Статус | Примечание |
|---|---|---|
| Проверки состояния | готово | `/health/livez` и `/health/readyz` добавлены в служебный HTTP-контур. |
| Метрики | готово | `/metrics` и gRPC-метрики подключены через общий `grpcserver`. |
| Структурированные логи | готово для процесса | Entry point и запуск серверов пишут JSON-логи без бизнес-данных. |
| Трассировка OpenTelemetry и проброс контекста | готово в общем runtime | `libs/go/grpcserver` подключает `otelgrpc.NewServerHandler` через `grpc.StatsHandler` и использует W3C `tracecontext+baggage`; экспорт трасс в OTel Collector не входит в этот срез и настраивается отдельно при начальной настройке наблюдаемости. |

## Синхронизация с параллельными доменами

| Домен | Когда синхронизироваться | Причина |
|---|---|---|
| `project-catalog` | Перед PKG-4.2 и PKG-5 | `services.yaml` и workspace policy могут ссылаться на руководящие пакеты и package sources, но проектная политика остаётся у `project-catalog`. |
| `provider-hub` | Перед PKG-4.2 | Репозитории-источники пакетов, webhook, PR и Git-истина пакета идут через provider-native контур. |
| `access-manager` | Перед PKG-2 | Действия доступа для источников, установок, верификации и секретов должны быть согласованы с access catalog. |
| `runtime-manager` и `fleet-manager` | Перед PKG-5 и PKG-7 | `package-hub` фиксирует установку и требования, но runtime-нагрузку, Kubernetes и размещение исполняет runtime/fleet контур. |
| `agent-manager` | Перед PKG-6 | Руководящие пакеты и возможности пакетов влияют на подготовку агентного контекста. |
| `billing-hub` | После PKG-5 | Ценовые метаданные и факты установки нужны для будущего учёта, но счета не живут в `package-hub`. |

## Критерии начала кода

- Принят пакет доменной документации `package-platform`.
- Для каждого следующего PR есть отдельный GitHub Issue.
- PR, который завершает Issue, содержит `Closes #...` в теле PR.
- Контрактный PR создаёт proto и AsyncAPI до реализации операций.
- Старый код из `deprecated/**` не используется как основа реализации.

## Критерии завершения домена

- `package-hub` имеет свой контур данных, миграций, контрактов и событий.
- Доступные пакеты, установленные пакеты, источники магазинов, версии, manifest, схемы секретов и верификация имеют авторитетные команды и чтения.
- Сервис публикует `package.*` события через outbox и `platform-event-log`.
- `agent-manager`, `runtime-manager`, `interaction-hub`, `billing-hub` и операторская консоль могут опираться на контракты `package-hub`.
- Документы и карты Issue обновлены, хвосты перенесены в следующие срезы явно.

## Апрув

- request_id: `owner-2026-05-06-package-platform-kickoff`
- Решение: approved
- Комментарий: план поставки `package-hub` согласован как целевое состояние стартового среза.
