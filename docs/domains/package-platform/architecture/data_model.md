---
doc_id: DM-CK8S-PACKAGE-0001
type: data-model
title: kodex — модель данных домена пакетной платформы
status: active
owner_role: SA
created_at: 2026-05-06
updated_at: 2026-05-07
related_issues: [642, 673, 680]
related_prs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-06-package-platform-kickoff"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-06
---

# Модель данных: пакетная платформа

## TL;DR

- Ключевые сущности: `PackageSource`, `PackageEntry`, `PackageVersion`, `PackageManifestSnapshot`, `PackageInstallation`, `PackageVerification`, `PackageSecretSchema`, `PackagePricingMetadata`.
- Технические агрегаты: `CommandResult`, `OutboxEvent`.
- Основные связи: источник магазина даёт доступные пакеты; пакет имеет версии; установка фиксирует выбранную версию, scope и статус; manifest задаёт требования.
- Риски миграций: нельзя хранить сырые секреты, канонические ссылки на заполненные секреты, исходники пакетов, runtime-нагрузку или биллинг-истину в БД `package-hub`.

## Правило пустых значений

`optional` в gRPC/request-контракте фиксирует наличие или отсутствие значения на транспортной границе. Это не означает, что соответствующая колонка в PostgreSQL обязана быть nullable.

В БД `NULL` используется только там, где отсутствие значения бизнесово отличается от пустого значения: внешние ссылки, необязательные временные метки, необязательные git-идентификаторы и ключи идемпотентности. Текстовые поля для безопасного отображения, описаний, ссылок и ref хранятся как `NOT NULL DEFAULT ''`, если пустая строка означает “не задано”.

## Сущности

### Правило версий агрегатов

Изменяемые агрегаты имеют монотонный маркер конкурентного изменения:
- `PackageSource.version`;
- `PackageEntry.version`;
- `PackageVersion.revision`, потому что поле `version_label` уже занято версией пакета из manifest;
- `PackageInstallation.version`;
- `PackagePricingMetadata.version`.

`PackageManifestSnapshot`, `PackageSecretSchema` и `PackageVerification` являются append-only записями. Изменение manifest, схемы секретов или результата проверки создаёт новую запись и обновляет версионируемый агрегат-владелец.

### PackageSource

`PackageSource` описывает подключённый источник доступных пакетов: магазин, пользовательский каталог или встроенный источник.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор источника. |
| `organization_id` | uuid | да | Внешняя ссылка на организацию из `access-manager`, если источник ограничен организацией. |
| `slug` | text | нет | Уникальный ключ источника в рамках scope. |
| `display_name` | text | нет | Название для пользователя. |
| `source_kind` | enum | нет | `built_in`, `store_package`, `custom_repository`, `proxy`. |
| `repository_ref` | text | да | Внешняя ссылка на репозиторий-источник или пакет магазина. |
| `catalog_endpoint_ref` | text | да | Логическая ссылка на endpoint магазина, если каталог берётся через пакет. |
| `status` | enum | нет | `active`, `disabled`, `blocked`, `sync_failed`. |
| `last_sync_at` | timestamptz | да | Последняя успешная синхронизация. |
| `last_error` | text | да | Короткая последняя ошибка. |
| `version` | bigint | нет | Оптимистичная конкуренция. |
| `created_at`, `updated_at` | timestamptz | нет | Технические временные метки. |

### PackageEntry

`PackageEntry` описывает пакет как каталоговую запись без привязки к конкретной установленной версии.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор пакета в локальном каталоге. |
| `source_id` | uuid | да | Источник, где пакет обнаружен; встроенные записи могут не иметь внешнего source. |
| `slug` | text | нет | Стабильный slug пакета. |
| `package_kind` | enum | нет | `plugin`, `guidance`, `store`, `platform_content`. |
| `publisher_ref` | text | да | Внешняя ссылка на издателя или организацию. |
| `display_name` | jsonb | нет | Локализованные названия. |
| `description` | jsonb | нет | Локализованные описания. |
| `icon_object_uri` | text | да | Ссылка на иконку в объектном хранилище или внешнем источнике. |
| `commercial_status` | enum | нет | `free`, `paid`, `restricted`, `unknown`. |
| `trust_status` | enum | нет | `built_in`, `verified`, `unverified`, `blocked`. |
| `status` | enum | нет | `available`, `hidden`, `revoked`, `blocked`. |
| `version` | bigint | нет | Оптимистичная конкуренция для изменения статуса, trust status и отображаемых метаданных. |
| `created_at`, `updated_at` | timestamptz | нет | Технические временные метки. |

### PackageVersion

`PackageVersion` фиксирует конкретную версию пакета и источник её получения.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор версии. |
| `package_id` | uuid | нет | Пакет-владелец. |
| `version_label` | text | нет | Версия по manifest или тегу. |
| `source_ref_kind` | enum | нет | `git_tag`, `git_commit`, `gitlink`, `proxy_ref`. |
| `source_ref` | text | нет | Тег, commit, gitlink или прокси-ссылка. |
| `source_commit_sha` | text | да | Commit репозитория-источника, если известен. |
| `manifest_digest` | text | нет | Digest проверенного manifest. |
| `verification_status` | enum | нет | `verified`, `unverified`, `rejected`, `revoked`. |
| `release_status` | enum | нет | `active`, `deprecated`, `revoked`, `blocked`. |
| `revision` | bigint | нет | Монотонная ревизия для ожидаемой версии при изменении статуса проверки, release status и manifest-ссылок. |
| `published_at` | timestamptz | да | Дата публикации версии, если известна. |
| `created_at`, `updated_at` | timestamptz | нет | Технические временные метки. |

### PackageManifestSnapshot

`PackageManifestSnapshot` хранит нормализованный снимок manifest для аудита и повторной проверки. Это не замена исходному файлу в репозитории.

При синхронизации доступного каталога новый снимок создаётся для новой версии пакета или при изменении source/manifest данных существующей версии. Повторная синхронизация без изменений не создаёт дубль снимка manifest.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор снимка. |
| `package_version_id` | uuid | нет | Версия пакета. |
| `schema_version` | int | нет | Версия схемы manifest. |
| `payload` | jsonb | нет | Нормализованный снимок manifest. |
| `validation_status` | enum | нет | `valid`, `invalid`, `warning`. |
| `validation_errors` | jsonb | нет | Структурированные ошибки и предупреждения. |
| `created_at` | timestamptz | нет | Когда снимок сохранён. |

### PackageInstallation

`PackageInstallation` фиксирует установленный пакет, выбранную версию и область применения.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор установки. |
| `package_id` | uuid | нет | Пакет. |
| `package_version_id` | uuid | нет | Установленная версия. |
| `scope_type` | enum | нет | `platform`, `organization`, `project`, `repository`. |
| `scope_ref` | text | нет | Внешний идентификатор scope. |
| `installation_status` | enum | нет | `requested`, `active`, `disabled`, `failed`, `uninstalled`. |
| `desired_state` | enum | нет | `present`, `absent`, `suspended`. |
| `runtime_requirement_digest` | text | да | Digest runtime-требований, если пакет требует runtime-нагрузку. |
| `secret_binding_status` | enum | нет | `not_required`, `missing`, `complete`, `invalid`. |
| `last_health_status` | enum | нет | `unknown`, `healthy`, `degraded`, `failed`. |
| `version` | bigint | нет | Оптимистичная конкуренция. |
| `created_at`, `updated_at` | timestamptz | нет | Технические временные метки. |

### PackageSecretSchema

`PackageSecretSchema` описывает поля секретов, которые нужно заполнить до запуска или активации пакета.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор схемы. |
| `package_version_id` | uuid | нет | Версия пакета. |
| `schema_digest` | text | нет | Digest схемы. |
| `fields` | jsonb | нет | Локализованные поля, типы, обязательность и подсказки. |
| `created_at` | timestamptz | нет | Когда схема сохранена. |

### PackageVerification

`PackageVerification` фиксирует append-only решение проверки версии пакета. Текущее состояние проверки хранится в `PackageVersion.verification_status`; команда проверки создаёт новую запись аудита и повышает `PackageVersion.revision`.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор проверки. |
| `package_version_id` | uuid | нет | Версия пакета. |
| `verification_status` | enum | нет | `verified`, `unverified`, `rejected`, `revoked`. |
| `verified_by_actor_ref` | text | да | Кто подтвердил или изменил статус. |
| `verification_notes` | text | да | Короткое пояснение. |
| `created_at` | timestamptz | нет | Когда решение проверки зафиксировано. |

### PackagePricingMetadata

`PackagePricingMetadata` хранит ценовые признаки для будущего биллинга. Это не счёт и не платёж.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор ценовой записи. |
| `package_id` | uuid | нет | Пакет. |
| `pricing_kind` | enum | нет | `free`, `paid`, `subscription`, `usage_based`, `restricted`. |
| `currency` | text | да | Валюта, если цена известна. |
| `price_payload` | jsonb | нет | Нормализованные ценовые параметры. |
| `version` | bigint | нет | Оптимистичная конкуренция для изменения ценовых метаданных. |
| `updated_at` | timestamptz | нет | Когда запись обновлена. |

### CommandResult

`CommandResult` хранит идемпотентный след команды в той же БД, где меняется агрегат. Повтор команды с тем же `command_id` или с той же парой `operation` + `idempotency_key` возвращает сохранённый результат, а не создаёт второе изменение. Запись `CommandResult` создаётся атомарно вместе с изменением агрегата, а не отдельной публичной операцией после факта.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `key` | text | нет | Первичный ключ идемпотентного следа. |
| `command_id` | uuid | да | Идемпотентный ключ команды, если клиент передал UUID команды. |
| `idempotency_key` | text | да | Альтернативный строковый ключ идемпотентности. |
| `operation` | text | нет | Имя операции. |
| `aggregate_type` | text | нет | Тип агрегата: `package_source`, `package`, `package_version`, `installation`, `verification`. |
| `aggregate_id` | uuid | нет | Идентификатор затронутого агрегата. |
| `result_payload` | jsonb | нет | Минимальный ответ для безопасного повтора команды. |
| `created_at` | timestamptz | нет | Время первого успешного выполнения. |

### OutboxEvent

`OutboxEvent` фиксируется в одной транзакции с изменением агрегата. Диспетчер публикует событие в `platform-event-log`, а потребители обрабатывают его через свой inbox/checkpoint.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор события. |
| `aggregate_type` | text | нет | Тип агрегата. |
| `aggregate_id` | uuid | нет | Идентификатор агрегата. |
| `event_type` | text | нет | Имя события `package.*`. |
| `schema_version` | int | нет | Версия схемы события. |
| `payload` | jsonb | нет | Минимальная полезная нагрузка события. |
| `occurred_at` | timestamptz | нет | Время доменного изменения. |
| `published_at` | timestamptz | да | Заполняется после успешной публикации. |
| `attempt_count` | int | нет | Счётчик попыток публикации. |
| `next_attempt_at` | timestamptz | нет | Когда событие можно снова забрать в доставку. |
| `locked_until` | timestamptz | да | Краткая аренда события текущим доставщиком. |
| `failed_permanently_at` | timestamptz | да | Когда событие переведено в постоянный сбой. |
| `failure_kind` | text | да | `transient` или `permanent`; пустое значение означает отсутствие сбоя. |
| `last_error` | text | да | Короткая последняя ошибка публикации для диагностики. |

## Связи

- `PackageSource` может быть источником многих `PackageEntry`.
- `PackageEntry` владеет `PackageVersion`, `PackagePricingMetadata` и каталоговыми метаданными.
- `PackageVersion` владеет `PackageManifestSnapshot`, `PackageSecretSchema` и append-only записями `PackageVerification`.
- `PackageInstallation` связывает выбранную `PackageVersion` с конкретным scope и хранит только статус заполненности секретов.
- Внутри БД `package-hub` допустимы обычные внешние ключи между своими таблицами.
- Ссылки на организации, проекты, репозитории, внешние аккаунты, кластеры и provider-native объекты хранятся как внешние идентификаторы без SQL-связей с чужими БД.
- Канонические ссылки на заполненные секреты хранит `access-manager` как `SecretBindingRef`; `package-hub` получает статус заполненности через команду, чтение или событие и не становится владельцем этих ссылок.

## Индексы и запросы

| Запрос | Нужные индексы |
|---|---|
| Список установленных пакетов по scope | `(scope_type, scope_ref, installation_status)` |
| Список доступных пакетов по источнику и виду | `(source_id, package_kind, status)` |
| Поиск пакета по slug | `(slug)` с учётом области источника. |
| Проверка установленной версии | `(package_id, package_version_id, scope_type, scope_ref)` |
| Поиск проблемных установок | `(installation_status, secret_binding_status, last_health_status)` |
| Поиск отозванных версий | `(verification_status, release_status)` |

## Миграционные ограничения

- Не создавать SQL-связи с БД `access-manager`, `project-catalog`, `provider-hub`, `runtime-manager` или `billing-hub`.
- Не хранить исходники пакетов и файлы руководства в PostgreSQL.
- Не хранить сырые секреты.
- Не использовать `jsonb` как единственное место для рабочих чтений, если поле участвует в фильтрах, правах, статусах или отображении.
- Установки и источники должны иметь версии для оптимистичной конкуренции.

## Апрув

- request_id: `owner-2026-05-06-package-platform-kickoff`
- Решение: approved
- Комментарий: модель данных домена пакетной платформы согласована как целевое состояние стартового среза.
