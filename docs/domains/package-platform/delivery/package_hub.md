---
doc_id: DLV-CK8S-PACKAGE-HUB
type: delivery-plan
title: kodex — поставка package-hub
status: active
owner_role: EM
created_at: 2026-05-06
updated_at: 2026-05-11
related_issues: [642, 646, 650, 663, 667, 670, 673, 678, 680, 684, 689, 692, 700]
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
| PKG-4.2 | #680 | Синхронизация доступного каталога и проверка manifest готовы. |
| PKG-5.1 | #684 | Запрос установки пакета и чтения установок готовы: `RequestPackageInstallation`, `GetPackageInstallation`, `ListPackageInstallations`, идемпотентность, проверка доступа, проверка готовности к установке и события `package.installation.requested/activated`. |
| PKG-5.2 | #689 | Изменение, отключение и снятие установки готовы: `UpdatePackageInstallation`, `DisablePackageInstallation`, `UninstallPackage`, ожидаемая версия и события жизненного цикла. |
| PKG-5.3a | #692 | Чтение схем секретов версий пакетов готово: снимки схем создаются из manifest при синхронизации каталога, `GetPackageSecretSchema` читает локальную схему с проверкой `package.secret.read`. |
| PKG-5.3b | не назначено | Сверка статуса заполненности секретов установки должна быть готова: `RefreshPackageInstallationSecretStatus` и связь с контуром секретов после согласования контракта заполненности секретов пакета в `access-manager`. |
| PKG-6.1 | #700 | Специализация видов пакетов готова: `plugin`, `guidance`, `store`, `platform_content`, правила manifest по виду и модели чтения через `package_kind`. |
| PKG-6.2+ | не назначено | Следующие специализированные сценарии руководящих пакетов, магазина и пользовательского контента платформы готовы без runtime-запуска и provider-native синхронизации. |
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
| `SyncAvailablePackages` | `ready` | PKG-4.2 |
| `GetPackage` | `ready` | PKG-3.4 |
| `ListPackages` | `ready` | PKG-3.4 |
| `GetPackageVersion` | `ready` | PKG-3.4 |
| `ListPackageVersions` | `ready` | PKG-3.4 |
| `GetPackageManifest` | `ready` | PKG-3.4 |
| `RequestPackageInstallation` | `ready` | PKG-5.1 |
| `UpdatePackageInstallation` | `ready` | PKG-5.2 |
| `DisablePackageInstallation` | `ready` | PKG-5.2 |
| `UninstallPackage` | `ready` | PKG-5.2 |
| `GetPackageInstallation` | `ready` | PKG-5.1 |
| `ListPackageInstallations` | `ready` | PKG-5.1 |
| `GetPackageSecretSchema` | `ready` | PKG-5.3a |
| `RefreshPackageInstallationSecretStatus` | `unimplemented` | PKG-5.3b |
| `SetPackageVerification` | `ready` | PKG-3.4 |

## Синхронизация каталога `PKG-4.2`

| Область | Статус | Примечание |
|---|---|---|
| Нормализованный снимок каталога | готово | `SyncAvailablePackages` принимает packages, versions и JSON-содержимое manifest, подготовленные адаптером источника. |
| Проверка manifest | готово | Сервис проверяет обязательные блоки manifest, локализованные метаданные, права из `libs/go/accesscatalog`, секреты, runtime-требования, уникальность slug/version и соответствие `manifest_digest` компактному JSON manifest. |
| Запись каталога | готово | Источник, пакеты, версии, новые снимки manifest, command result и outbox пишутся в одной PostgreSQL-транзакции. |
| События | готово | Публикуются `package.catalog.synced`, `package.package.discovered/updated`, `package.version.discovered/updated`. |
| Получение из внешнего store/provider | следующий срез интеграции | Реальный обход Git/store/provider остаётся вне `package-hub`: внешний адаптер готовит нормализованный снимок и вызывает gRPC-команду. |

## Установки пакетов `PKG-5.1`

| Область | Статус | Примечание |
|---|---|---|
| Запрос установки | готово | `RequestPackageInstallation` проверяет права `package.install`, существование пакета и версии, статус пакета, статус версии, manifest и создаёт установку в заданной области. |
| Идемпотентность | готово | Повтор команды по `command_id` или `idempotency_key` возвращает сохранённый снимок установки только после сверки входных параметров и повторной проверки права чтения. |
| Начальный статус | готово | Установка становится `active`, если package manifest не требует runtime-нагрузку и обязательные секреты; иначе получает статус `requested`. |
| События | готово | Команда пишет `package.installation.requested` или `package.installation.activated` через outbox в той же транзакции, что и установка и command result. |
| Чтения | готово | `GetPackageInstallation` и `ListPackageInstallations` проверяют `package.installation.read` и читают локальную PostgreSQL-проекцию установок. |
| Не входит в срез | запланировано | Изменение версии, отключение, снятие установки, обновление статуса секретов и runtime-запуск идут отдельными срезами, чтобы не смешивать доменную запись установки с соседними runtime и контуром секретов. |

## Жизненный цикл установок `PKG-5.2`

| Область | Статус | Примечание |
|---|---|---|
| Изменение установки | готово | `UpdatePackageInstallation` меняет выбранную версию, desired state и безопасные статусы `requested`, `active`, `failed`; статусы `disabled` и `uninstalled` доступны только через отдельные команды. |
| Пересчёт требований | готово | При смене версии `package-hub` проверяет пакет и версию, читает последний manifest, пересчитывает `runtime_requirement_digest`, статус секретов и сбрасывает health в `unknown`. |
| Отключение установки | готово | `DisablePackageInstallation` переводит установку в `disabled`, desired state в `suspended` и публикует `package.installation.disabled`. |
| Снятие установки | готово | `UninstallPackage` переводит установку в `uninstalled`, desired state в `absent` и публикует `package.installation.uninstalled`. |
| Конкурентность и идемпотентность | готово | Все три команды требуют expected version, сохраняют command result и outbox-событие в одной PostgreSQL-транзакции. |
| Не входит в срез | запланировано | Фактическое снятие Kubernetes workloads остаётся в runtime-контуре; сверка заполненности секретов остаётся в PKG-5.3b. |

## Схемы секретов `PKG-5.3a`

| Область | Статус | Примечание |
|---|---|---|
| Снимки схем секретов | готово | `SyncAvailablePackages` извлекает блок `secrets` из проверенного manifest, нормализует локализованные поля и сохраняет новую `PackageSecretSchema` только при новом digest. |
| События | готово | Новая схема публикует `package.secret_schema.updated` через outbox вместе с остальными событиями синхронизации каталога. |
| Чтение схемы | готово | `GetPackageSecretSchema` проверяет `package.secret.read` на ресурс схемы версии пакета и возвращает последнюю локальную схему. |
| Не входит в срез | запланировано | `RefreshPackageInstallationSecretStatus` требует согласованного контракта `access-manager` для проверки заполненности секретов пакета и остаётся отдельным срезом. |

## Виды пакетов `PKG-6.1`

| Область | Статус | Примечание |
|---|---|---|
| Доменная модель | готово | Закрытый enum `PackageKind` уже содержит `plugin`, `guidance`, `store`, `platform_content`; произвольные строковые виды не допускаются. |
| Проверка manifest | готово | `SyncAvailablePackages` сверяет `CatalogPackageSnapshot.package_kind` с `identity.kind` и применяет правила вида: `guidance` не требует runtime/секреты/API/действия, `store` требует capability `store`, `platform_content` требует capability `platform_content` без секретов/API/действий, `plugin` не использует capability других видов. |
| Модели чтения | готово | `ListPackages` и `ListPackageInstallations` принимают фильтр `package_kind`; невалидный enum отклоняется до авторизации и чтения БД. |
| Не входит в срез | запланировано | Runtime-запуск пакетов, получение каталога из Git/store/provider и сверка заполненности секретов остаются в отдельных срезах. |

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
| `runtime-manager` и `fleet-manager` | Перед runtime-срезом установок и PKG-7 | `package-hub` фиксирует установку и требования, но runtime-нагрузку, Kubernetes и размещение исполняет runtime/fleet контур. |
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
