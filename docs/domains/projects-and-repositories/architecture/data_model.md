---
doc_id: DM-CK8S-PROJ-0001
type: data-model
title: kodex — модель данных домена проектов и репозиториев
status: active
owner_role: SA
created_at: 2026-05-05
updated_at: 2026-05-05
related_issues: [628, 629, 630, 631, 632, 633]
related_prs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-05-wave8-project-catalog-kickoff"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-05
---

# Модель данных: проекты и репозитории

## TL;DR

- Ключевые сущности: `Project`, `RepositoryBinding`, `ServicesPolicy`, `DocumentationSource`, `BranchRules`, `ReleasePolicy`, `ReleaseLine`, `PlacementPolicy`.
- Основные связи: проект владеет репозиториями и политикой; репозиторий может иметь свои уточняющие правила; источники документации связываются с проектом, репозиторием или сервисом.
- Риски миграций: нельзя хранить чужие provider-native сущности как канонические данные; нельзя делать SQL-связи с БД других сервисов.

## Сущности

### Project

| Поле | Тип | Nullable | Notes |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор проекта. |
| `organization_id` | uuid | нет | Внешняя ссылка на организацию из `access-manager`. |
| `slug` | text | нет | Уникален в рамках организации. |
| `display_name` | text | нет | Название для пользователя. |
| `description` | text | да | Описание проекта. |
| `status` | enum | нет | `active`, `archived`, `disabled`. |
| `version` | bigint | нет | Оптимистичная конкуренция. |
| `created_at`, `updated_at` | timestamptz | нет | Технические timestamps. |

### RepositoryBinding

| Поле | Тип | Nullable | Notes |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор привязки репозитория. |
| `project_id` | uuid | нет | Внутренняя ссылка на проект. |
| `provider` | text | нет | `github`, позже `gitlab`. |
| `provider_owner` | text | нет | Владелец или группа у провайдера. |
| `provider_name` | text | нет | Имя репозитория у провайдера. |
| `default_branch` | text | нет | Ветка по умолчанию по данным провайдера или политики. |
| `status` | enum | нет | `active`, `pending`, `blocked`, `archived`. |
| `provider_repository_id` | text | да | Внешний идентификатор провайдера, если доступен. |
| `version` | bigint | нет | Оптимистичная конкуренция. |

### ServicesPolicy

| Поле | Тип | Nullable | Notes |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор политики. |
| `project_id` | uuid | нет | Проект-владелец. |
| `source_repository_id` | uuid | да | Где найден исходный `services.yaml`. |
| `source_path` | text | нет | Путь к файлу политики. |
| `policy_version` | bigint | нет | Версия проверенного снимка. |
| `content_hash` | text | нет | Хэш исходного содержимого. |
| `validated_payload` | jsonb | нет | Типизированное содержимое после валидации; в коде должно иметь именованные структуры. |
| `validation_status` | enum | нет | `valid`, `invalid`, `stale`. |

### DocumentationSource

| Поле | Тип | Nullable | Notes |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор источника. |
| `project_id` | uuid | нет | Проект. |
| `repository_id` | uuid | да | Репозиторий, если источник живёт в репозитории проекта. |
| `scope_type` | enum | нет | `project`, `service`, `dependency`, `guidance_ref`. |
| `scope_id` | text | да | Сервис, зависимость или другой scope. |
| `local_path` | text | нет | Куда источник должен попадать в рабочий контур. |
| `access_mode` | enum | нет | `read`, `write`. |
| `status` | enum | нет | `active`, `disabled`, `blocked`. |

### BranchRules

| Поле | Тип | Nullable | Notes |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор правил. |
| `project_id` | uuid | нет | Проект. |
| `repository_id` | uuid | да | Если правила применяются к конкретному репозиторию. |
| `pattern` | text | нет | Шаблон ветки. |
| `required_checks` | text[] | нет | Имена обязательных проверок. |
| `merge_policy` | enum | нет | `merge`, `squash`, `rebase`, `manual`. |
| `status` | enum | нет | `active`, `disabled`. |

### ReleasePolicy и ReleaseLine

| Поле | Тип | Nullable | Notes |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор политики или линии. |
| `project_id` | uuid | нет | Проект. |
| `name` | text | нет | Название линии или политики. |
| `branch_pattern` | text | нет | Шаблон релизной ветки. |
| `rollout_strategy` | enum | нет | Стратегия выкладки: `direct`, `staged`, `canary`. |
| `rollback_policy` | enum | нет | Политика отката: `manual`, `automatic_on_gate`, `automatic_on_alert`. |
| `risk_profile_ref` | text | да | Ссылка на риск-профиль в домене governance. |
| `status` | enum | нет | `active`, `disabled`, `archived`. |

### PlacementPolicy

| Поле | Тип | Nullable | Notes |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор политики. |
| `project_id` | uuid | нет | Проект. |
| `repository_id` | uuid | да | Конкретный репозиторий, если политика уже проекта. |
| `service_key` | text | да | Конкретный сервис из `services.yaml`. |
| `allowed_cluster_refs` | text[] | нет | Внешние ссылки на контуры `fleet-manager`. |
| `status` | enum | нет | `active`, `disabled`. |

## Связи

- `Project` владеет `RepositoryBinding`, `ServicesPolicy`, `DocumentationSource`, `BranchRules`, `ReleasePolicy`, `ReleaseLine`, `PlacementPolicy`.
- Внутри БД `project-catalog` допустимы обычные внешние ключи между своими таблицами.
- Ссылки на организации, кластеры, роли, агентные процессы и provider-native сущности хранятся как внешние идентификаторы без SQL-связей с чужими БД.

## Индексы и запросы

| Запрос | Индексы |
|---|---|
| Список проектов организации | `(organization_id, status, slug)` |
| Список репозиториев проекта | `(project_id, status, provider, provider_owner, provider_name)` |
| Поиск репозитория по provider identity | `(provider, provider_owner, provider_name)` unique для активной привязки |
| Источники документации для рабочего контура | `(project_id, scope_type, scope_id, status)` |
| Активные правила веток | `(project_id, repository_id, status)` |
| Активные релизные политики | `(project_id, status)` |

## Политика хранения данных

- Архивные проекты и репозитории не удаляются физически в MVP, чтобы сохранить аудит и связи с provider-native артефактами.
- Старые версии `ServicesPolicy` могут храниться как история изменений с ограничением срока хранения, если содержимое станет большим.
- Сырые секреты, токены провайдера и содержимое приватных файлов не хранятся в домене.

## Апрув

- request_id: `owner-2026-05-05-wave8-project-catalog-kickoff`
- Решение: approved
- Комментарий: модель данных домена проектов и репозиториев согласована как целевое состояние стартового среза.
