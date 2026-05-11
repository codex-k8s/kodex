---
doc_id: API-CK8S-PACKAGE-0001
type: api-contract
title: kodex — API-обзор package-hub
status: active
owner_role: SA
created_at: 2026-05-06
updated_at: 2026-05-11
related_issues: [642, 646, 650, 673, 678, 680, 684, 689, 692, 700, 704, 706, 711]
related_prs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-06-package-platform-kickoff"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-06
---

# API-обзор: package-hub

## TL;DR

- Тип API: внутренний gRPC `PackageHubService`, доменные события `package.*`.
- Аутентификация: через gateway, сервисный токен или MCP-границу; доменные команды и авторитетные чтения дополнительно проверяются через `access-manager`.
- Версионирование: стабильный транспортный `v1`; источники правды будущих контрактов — proto и AsyncAPI.
- Основные операции: источники пакетов, синхронизация доступного каталога, чтения пакетов и версий, установки, схемы секретов, статус верификации.

## Спецификации

- gRPC proto: `proto/kodex/packages/v1/package_hub.proto`.
- AsyncAPI: `specs/asyncapi/package-hub.v1.yaml`.
- Внешний HTTP для операторской и администраторской консоли: через тонкий `staff-gateway` с OpenAPI-контрактом, не напрямую из доменного сервиса.

Этот документ фиксирует обзор операций и событий. Фактическими источниками правды для транспорта станут proto и AsyncAPI; если описание ниже расходится с машинной спецификацией, исправляется документ или контракт в том же изменении.

## Операции

| Операция | Вид | Доступ | Идемпотентность | Примечание |
|---|---|---|---|---|
| `ConnectPackageSource` | gRPC command | `package.source.connect` | `CommandMeta.command_id` или `idempotency_key` | Подключает источник магазина, пользовательский источник или встроенный источник. |
| `UpdatePackageSource` | gRPC command | `package.source.update` | `CommandMeta.command_id` или `idempotency_key` + ожидаемая версия | Обновляет имя, ссылку endpoint, параметры источника и неотключающие статусы; `disabled` разрешён только через `DisablePackageSource`. |
| `DisablePackageSource` | gRPC command | `package.source.disable` | `CommandMeta.command_id` или `idempotency_key` + ожидаемая версия | Отключает источник без физического удаления. |
| `GetPackageSource` | gRPC query | `package.source.read` | нет | Авторитетное чтение источника. |
| `ListPackageSources` | gRPC query | `package.source.read` | нет | Список источников по scope и статусу. |
| `SyncAvailablePackages` | gRPC command | `package.catalog.sync` | `CommandMeta.command_id` | Сохраняет нормализованный снимок доступного каталога из источника: packages, versions и manifest payload. |
| `GetPackage` | gRPC query | `package.catalog.read` | нет | Читает пакетную запись. |
| `ListPackages` | gRPC query | `package.catalog.read` | нет | Фильтрует доступные пакеты по виду, источнику, статусу и коммерческому признаку. |
| `GetPackageVersion` | gRPC query | `package.catalog.read` | нет | Читает конкретную версию, manifest и статус проверки. |
| `ListPackageVersions` | gRPC query | `package.catalog.read` | нет | Список версий пакета. |
| `GetPackageManifest` | gRPC query | `package.manifest.read` | нет | Возвращает нормализованный снимок manifest. |
| `RequestPackageInstallation` | gRPC command | `package.install` | `CommandMeta.command_id` или `idempotency_key` | Создаёт запрос установки пакета в заданной области; если активная установка уже есть, возвращает `already_exists`, а изменение идёт через `UpdatePackageInstallation` с ожидаемой версией. |
| `UpdatePackageInstallation` | gRPC command | `package.installation.update` | `CommandMeta.command_id` или `idempotency_key` + ожидаемая версия | Меняет статус, desired state или выбранную версию установки. |
| `DisablePackageInstallation` | gRPC command | `package.installation.disable` | `CommandMeta.command_id` или `idempotency_key` + ожидаемая версия | Отключает установленный пакет без удаления истории. |
| `UninstallPackage` | gRPC command | `package.uninstall` | `CommandMeta.command_id` или `idempotency_key` + ожидаемая версия | Переводит установку в `uninstalled` и публикует событие. |
| `GetPackageInstallation` | gRPC query | `package.installation.read` | нет | Авторитетное чтение установки. |
| `ListPackageInstallations` | gRPC query | `package.installation.read` | нет | Список установок по области, статусу и виду пакета. |
| `GetPackageSecretSchema` | gRPC query | `package.secret.read` | нет | Читает схему секретов версии пакета. |
| `RefreshPackageInstallationSecretStatus` | gRPC command | `package.installation.update` | ожидаемая версия | Перечитывает состояние привязок секретов из `access-manager`, проверяет доступность через `secretresolver.Checker` без возврата значения и обновляет только статус заполненности установки. |
| `SetPackageVerification` | gRPC command | `package.verify` | `CommandMeta.command_id` или `idempotency_key` + ожидаемая ревизия версии пакета | Фиксирует верификацию, отклонение или отзыв версии пакета. |

`SyncAvailablePackages` не ходит во внешний Git/store/provider напрямую. Эту работу выполняет адаптер источника: он получает данные, приводит их к нормализованному контракту снимка и вызывает `package-hub`. Так сервис остаётся владельцем локального каталога и проверки manifest, но не становится Git-клиентом магазина. `manifest_digest` сверяется как `sha256:<hex>` от компактного нормализованного JSON manifest; несовпадение считается невалидным снимком. `required_access_actions` сверяются с общим каталогом системных действий `libs/go/accesscatalog`; неизвестный ключ отклоняется как невалидный manifest.

## Виды пакетов в API

Транспортный `PackageKind` является закрытым enum: `plugin`, `guidance`, `store`, `platform_content`. Произвольные строковые виды не принимаются.

| Контракт | Как используется вид пакета |
|---|---|
| `CatalogPackageSnapshot.package_kind` | Внешний адаптер источника передаёт вид пакета вместе с нормализованным manifest. `package-hub` сверяет его с `identity.kind` внутри manifest. |
| `PackageEntry.package_kind` | Локальная каталоговая запись хранит вид пакета как часть авторитетной модели чтения. |
| `ListPackages.package_kind` | Фильтр каталога по виду пакета; невалидный enum отклоняется до проверки доступа и чтения БД. |
| `ListPackageInstallations.package_kind` | Фильтр установок по виду пакета через связь установки с каталоговой записью пакета. |

Для руководящих пакетов отдельный transport-контракт не вводится. Текущий сценарий чтения покрывается так:

| Сценарий | Контракт |
|---|---|
| Найти доступные руководящие пакеты | `ListPackages(package_kind=guidance)` |
| Найти установленные руководящие пакеты в scope | `ListPackageInstallations(package_kind=guidance, scope=...)` |
| Прочитать правила получения и состав пакета | `GetPackageManifest(package_version_id)` |

`package-hub` отдаёт только пакетную истину: запись каталога, установку, выбранную версию и проверенный manifest. Подготовка workspace, checkout источника и mount локальных документов остаются за `agent-manager` и runtime-контуром.

Для пакета магазина и пользовательского контента платформы отдельный transport-контракт также не вводится:

| Сценарий | Контракт |
|---|---|
| Найти доступные пакеты магазина | `ListPackages(package_kind=store)` |
| Найти установленный магазин в scope | `ListPackageInstallations(package_kind=store, scope=...)` |
| Прочитать endpoint, источник и требования пакета магазина | `GetPackageManifest(package_version_id)` и `GetPackageSource` |
| Найти пакет сайта или пользовательской документации платформы | `ListPackages(package_kind=platform_content)` |
| Найти установленный пакет пользовательского контента платформы | `ListPackageInstallations(package_kind=platform_content, scope=...)` |
| Прочитать состав пакета пользовательского контента | `GetPackageManifest(package_version_id)` |

`package-hub` хранит локальное состояние источника, пакета, версии, установки, manifest и статусы. Он не выдаёт каталог внешнего магазина в реальном времени, не становится бизнес-системой магазина, не ходит в Git/provider и не хранит файлы сайта или документации в БД.

`package-hub` не получает сырые значения секретов установки. Для пересчёта заполненности он должен получить от `access-manager` канонические ссылки на привязки секретов и использовать только `libs/go/secretresolver.Checker`, который проверяет доступность без возврата самого секрета вызывающему коду. Checker может работать с `kubernetes_mounted_secret`, `env` и `vault`, но эти детали реализации не становятся частью доменной модели `package-hub`. Для Vault проверка безопасно подтверждает наличие пути и текущей версии через запрос только метаданных, но не читает поле `#key`. `Resolver.Resolve` в пакетном домене запрещён, пока отдельный пакетный runtime не получит собственный согласованный контур исполнения.

Manifest дополнительно проверяется по виду пакета:

- `plugin` не должен выдавать себя за `guidance`, `store` или `platform_content` через зарезервированные capability.
- `guidance` должен иметь capability `guidance`, не должен иметь `store` или `platform_content` и не должен требовать runtime, секреты, действия доступа или API платформы.
- `store` должен иметь capability `store` и не должен иметь `guidance` или `platform_content`.
- `platform_content` должен иметь capability `platform_content`, не должен иметь `guidance` или `store` и не должен требовать секреты, действия доступа или API платформы.

## Модель ошибок

| Ошибка | Когда возвращается |
|---|---|
| `invalid_argument` | Невалидный slug, область, manifest, версия, source ref или схема секретов. |
| `permission_denied` | `access-manager` запретил действие. |
| `not_found` | Источник, пакет, версия или установка не найдены. |
| `already_exists` | Дубликат slug источника, slug пакета или активной установки в той же области. |
| `failed_precondition` | Нельзя установить отозванную версию, заблокированный пакет, версию без manifest или пакет, запрещённый policy. |
| `aborted` | Конфликт ожидаемой версии. |
| `unavailable` | Временная ошибка зависимости, БД или источника каталога. |

## События

События фиксируют бизнес-факты жизненного цикла, а не полный CRUD. Физическое удаление не входит в штатный `v1`: вместо `deleted` используются отключение, отзыв и снятие установки.

| Event | Aggregate | Payload минимум |
|---|---|---|
| `package.source.connected` | package_source | `source_id`, `source_kind`, `status`, `version` |
| `package.source.updated` | package_source | `source_id`, `status`, `version` |
| `package.source.disabled` | package_source | `source_id`, `status`, `version` |
| `package.catalog.synced` | package_source | `source_id`, `synced_at`, `package_count`, `version_count` |
| `package.package.discovered` | package | `package_id`, `source_id`, `slug`, `package_kind` |
| `package.package.updated` | package | `package_id`, `slug`, `status`, `trust_status`, `version` |
| `package.version.discovered` | package_version | `package_id`, `package_version_id`, `version_label`, `manifest_digest` |
| `package.version.updated` | package_version | `package_id`, `package_version_id`, `verification_status`, `release_status`, `revision` |
| `package.version.revoked` | package_version | `package_id`, `package_version_id`, `reason_code`, `revision` |
| `package.verification.updated` | package_version | `package_id`, `package_version_id`, `verification_status`, `revision` |
| `package.installation.requested` | package_installation | `installation_id`, `package_id`, `package_version_id`, `scope_type`, `scope_ref` |
| `package.installation.activated` | package_installation | `installation_id`, `package_id`, `package_version_id`, `scope_type`, `scope_ref` |
| `package.installation.updated` | package_installation | `installation_id`, `installation_status`, `desired_state`, `version` |
| `package.installation.disabled` | package_installation | `installation_id`, `installation_status`, `version` |
| `package.installation.uninstalled` | package_installation | `installation_id`, `installation_status`, `version` |
| `package.secret_schema.updated` | package_version | `package_id`, `package_version_id`, `schema_digest`, `revision` |

## Состояние реализации

| Область | Статус |
|---|---|
| Доменная документация | Подготовлена как целевой стартовый срез. |
| gRPC proto `PackageHubService` | Подготовлен как стабильный транспортный `v1` в `proto/kodex/packages/v1/package_hub.proto`. |
| AsyncAPI `package.*` | Подготовлен как стабильный событийный `v1` в `specs/asyncapi/package-hub.v1.yaml`. |
| Go-артефакты gRPC | Генерируются в `proto/gen/go/kodex/packages/v1/**`. |
| Go-артефакты событий | Генерируются в `libs/go/platformevents/packagehub/events.gen.go`. |
| Сервисный процесс `package-hub` | Общий gRPC runtime, служебные `/health/*`, `/metrics`, PostgreSQL repository, проверка доступа через `access-manager` и часть операций `PackageHubService` подключены. |
| PostgreSQL и outbox | Таблицы package-каталога, установок, проверок, идемпотентного следа и outbox добавлены; диспетчер публикует события через `platform-event-log`. |
| Реализованные операции | `ConnectPackageSource`, `UpdatePackageSource`, `DisablePackageSource`, `GetPackageSource`, `ListPackageSources`, `SyncAvailablePackages`, `GetPackage`, `ListPackages`, `GetPackageVersion`, `ListPackageVersions`, `GetPackageManifest`, `GetPackageSecretSchema`, `RequestPackageInstallation`, `UpdatePackageInstallation`, `DisablePackageInstallation`, `UninstallPackage`, `GetPackageInstallation`, `ListPackageInstallations`, `SetPackageVerification`. |
| Операции следующих срезов | `RefreshPackageInstallationSecretStatus` и runtime-связанные команды пока возвращают `unimplemented`; общий checker без возврата значения для будущей проверки доступности секретов уже доступен в `libs/go/secretresolver`, но контракт выдачи ссылок установки остаётся за `access-manager`. |

## Совместимость

- Стабильный `v1` контракт не должен удалять поля без цикла `deprecate -> migrate -> remove`.
- gRPC-контракт не импортирует transport DTO в домен; преобразование должно жить в transport caster слое.
- События должны проектироваться так, чтобы переход с PostgreSQL event log на брокер не ломал доменные контракты.

## Апрув

- request_id: `owner-2026-05-06-package-platform-kickoff`
- Решение: approved
- Комментарий: API-обзор `package-hub` согласован как целевое состояние стартового среза; стабильные transport-спецификации создаются отдельным срезом.
