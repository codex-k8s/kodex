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

- Ключевые сущности: `Project`, `RepositoryBinding`, `ServicesPolicy`, `ServiceDescriptor`, `DocumentationSource`, `BranchRules`, `ReleasePolicy`, `ReleaseLine`, `PlacementPolicy`, `PolicyOverride`.
- Технические агрегаты: `CommandResult`, `OutboxEvent`.
- Основные связи: проект владеет репозиториями и политикой; репозиторий может иметь свои уточняющие правила; источники документации связываются с проектом, репозиторием или сервисом.
- Риски миграций: нельзя хранить чужие provider-native сущности как канонические данные; нельзя делать SQL-связи с БД других сервисов.

## Правило пустых значений

`optional` в gRPC/request-контракте фиксирует наличие или отсутствие значения на транспортной границе. Это не означает, что соответствующая колонка в PostgreSQL обязана быть nullable.

В БД `NULL` используется только там, где отсутствие значения бизнесово отличается от пустого значения: внешние ссылки, необязательные временные метки, необязательные provider-native идентификаторы и ключи идемпотентности. Текстовые поля для безопасного отображения, описаний, ссылок на изображения, ref и scope ref хранятся как `NOT NULL DEFAULT ''`, если пустая строка означает “не задано”. Это упрощает индексы, фильтры и чтения для пользовательского интерфейса без размывания бизнес-семантики.

## Сущности

### Project

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор проекта. |
| `organization_id` | uuid | нет | Внешняя ссылка на организацию из `access-manager`. |
| `slug` | text | нет | Уникален в рамках организации. |
| `display_name` | text | нет | Название для пользователя. |
| `description` | text | да | Описание проекта. |
| `icon_object_uri` | text | да | Ссылка на объект изображения в бакете, например иконка проекта; бинарные данные не хранятся в БД. |
| `status` | enum | нет | `active`, `archived`, `disabled`. |
| `version` | bigint | нет | Оптимистичная конкуренция. |
| `created_at`, `updated_at` | timestamptz | нет | Технические временные метки. |

### RepositoryBinding

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор привязки репозитория. |
| `project_id` | uuid | нет | Внутренняя ссылка на проект. |
| `provider` | text | нет | `github`, позже `gitlab`. |
| `provider_owner` | text | нет | Владелец или группа у провайдера. |
| `provider_name` | text | нет | Имя репозитория у провайдера. |
| `default_branch` | text | нет | Ветка по умолчанию по данным провайдера или политики. |
| `status` | enum | нет | `active`, `pending`, `blocked`, `archived`. |
| `provider_repository_id` | text | да | Внешний идентификатор провайдера, если доступен. |
| `icon_object_uri` | text | да | Ссылка на объект изображения в бакете, например иконка репозитория; бинарные данные не хранятся в БД. |
| `version` | bigint | нет | Оптимистичная конкуренция. |

### ServicesPolicy

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор политики. |
| `project_id` | uuid | нет | Проект-владелец. |
| `source_repository_id` | uuid | да | Где найден исходный `services.yaml`. |
| `source_path` | text | нет | Путь к файлу политики. |
| `source_ref` | text | да | Ветка, тег или другой ref, откуда импортирована политика. |
| `source_commit_sha` | text | нет | Commit, из которого импортирована проверенная политика. |
| `source_blob_sha` | text | да | Хэш объекта файла у провайдера, если доступен. |
| `policy_version` | bigint | нет | Версия проверенного снимка. |
| `content_hash` | text | нет | Хэш исходного содержимого. |
| `validated_payload` | jsonb | нет | Нормализованный типизированный снимок исходной политики для аудита и повторной валидации; не является основным контуром чтения для сервисов. |
| `validation_status` | enum | нет | `valid`, `invalid`, `stale`. |
| `projection_status` | enum | нет | `synced`, `pending`, `failed`, `overridden`. |
| `imported_at` | timestamptz | нет | Когда проекция была сохранена в БД. |

### ServiceDescriptor

`ServiceDescriptor` — типизированная и индексируемая часть проверенного `services.yaml`. `project-catalog` строит этот набор из нормализованного `validated_payload`, а не принимает его как чужую каноническую истину. Код не должен каждый раз разбирать `validated_payload` ради рабочих чтений, привязки документации или политики размещения.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор сервиса внутри каталога. |
| `project_id` | uuid | нет | Проект-владелец. |
| `services_policy_id` | uuid | нет | Проверенная версия политики, из которой получен сервис. |
| `repository_id` | uuid | да | Репозиторий, где расположен сервис, если сервис привязан к конкретному репозиторию. |
| `service_key` | text | нет | Стабильный ключ сервиса из `services.yaml`. |
| `display_name` | text | нет | Человекочитаемое имя сервиса. |
| `kind` | enum | нет | `backend`, `frontend`, `worker`, `documentation`, `package`, `other`. |
| `root_path` | text | нет | Корневой путь сервиса в рабочем контуре. |
| `documentation_scope_id` | text | да | Ключ для связывания с `DocumentationSource.scope_id`. |
| `depends_on_service_keys` | text[] | нет | Зависимости от других сервисов проекта по ключам. |
| `status` | enum | нет | `active`, `disabled`, `stale`. |
| `version` | bigint | нет | Оптимистичная конкуренция для точечных обновлений контура чтения. |

### DocumentationSource

| Поле | Тип | Может быть пустым | Примечание |
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

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор правил. |
| `project_id` | uuid | нет | Проект. |
| `repository_id` | uuid | да | Если правила применяются к конкретному репозиторию. |
| `pattern` | text | нет | Шаблон ветки. |
| `required_checks` | text[] | нет | Имена обязательных проверок. |
| `merge_policy` | enum | нет | `merge`, `squash`, `rebase`, `manual`. |
| `status` | enum | нет | `active`, `disabled`. |

### ReleasePolicy

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор релизной политики. |
| `project_id` | uuid | нет | Проект. |
| `name` | text | нет | Название политики. |
| `branch_pattern` | text | нет | Шаблон релизной ветки. |
| `rollout_strategy` | enum | нет | Стратегия выкладки: `direct`, `staged`, `canary`. |
| `rollback_policy` | enum | нет | Политика отката: `manual`, `automatic_on_gate`, `automatic_on_alert`. |
| `risk_profile_ref` | text | да | Ссылка на риск-профиль в домене governance. |
| `status` | enum | нет | `active`, `disabled`, `archived`. |

### ReleaseLine

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор релизной линии. |
| `project_id` | uuid | нет | Проект. |
| `release_policy_id` | uuid | нет | Релизная политика, по которой живёт линия. |
| `name` | text | нет | Название линии. |
| `branch_pattern` | text | нет | Шаблон релизной ветки. |
| `status` | enum | нет | `active`, `disabled`, `archived`. |

### PlacementPolicy

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор политики. |
| `project_id` | uuid | нет | Проект. |
| `repository_id` | uuid | да | Конкретный репозиторий, если политика уже проекта. |
| `service_key` | text | да | Конкретный сервис из `services.yaml`. |
| `allowed_cluster_refs` | text[] | нет | Внешние ссылки на контуры `fleet-manager`. |
| `status` | enum | нет | `active`, `disabled`. |

### PolicyOverride

`PolicyOverride` описывает аварийное временное отклонение от политики, управляемой через Git. Это не основной путь изменения `services.yaml`: штатные изменения создаются через PR, проходят ревью и затем импортируются в `project-catalog`.

Активное и не истёкшее переопределение не должно оставаться только аудиторской строкой. Оно возвращается отдельным авторитетным чтением и включается в `WorkspacePolicy` как явное отклонение от политики из Git. Целевой потребитель применяет семантику `payload` только для своего `target_type`; если семантика ещё не поддержана, потребитель обязан явно показать или заблокировать ручное отклонение, а не молча игнорировать его.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор переопределения. |
| `project_id` | uuid | нет | Проект-владелец. |
| `target_type` | enum | нет | `services_policy`, `branch_rules`, `release_policy`, `release_line`, `placement_policy`, `documentation_source`. |
| `target_id` | uuid | да | Конкретный агрегат, если переопределение привязано к нему. |
| `payload` | jsonb | нет | Минимальный набор временно переопределённых параметров. |
| `reason` | text | нет | Причина аварийного изменения. |
| `status` | enum | нет | `active`, `expired`, `cancelled`. |
| `expires_at` | timestamptz | нет | Срок действия переопределения. |
| `created_by_actor_ref` | text | нет | Внешняя ссылка на инициатора для аудита. |
| `created_at`, `updated_at` | timestamptz | нет | Технические временные метки. |

### PolicyEditProposal

`PolicyEditProposal` фиксирует запрос на изменение `services.yaml` через PR. Он не меняет операционную проекцию напрямую: после слияния PR политика импортируется отдельной командой.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор предложения. |
| `project_id` | uuid | нет | Проект-владелец. |
| `repository_id` | uuid | нет | Репозиторий, где должен быть изменён `services.yaml`. |
| `source_path` | text | нет | Путь к файлу политики. |
| `requested_changes` | jsonb | нет | Типизированная полезная нагрузка `PolicyEditProposalRequestedChanges`: summary и список ожидаемых изменений. |
| `status` | text | нет | Машиночитаемый статус предложения. |
| `created_at` | timestamptz | нет | Когда предложение создано. |

### CommandResult

`CommandResult` хранит идемпотентный след команды в той же БД, где меняется агрегат. Повтор команды с тем же `command_id` возвращает сохранённый результат, а не создаёт второе изменение.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `key` | text | нет | Первичный ключ идемпотентного следа. |
| `command_id` | uuid | да | Идемпотентный ключ команды, если клиент передал UUID команды. |
| `idempotency_key` | text | да | Альтернативный строковый ключ идемпотентности. |
| `operation` | text | нет | Имя операции, к которой относится ключ. |
| `aggregate_type` | text | нет | Тип агрегата: `project`, `repository`, `services_policy`, `documentation_source`, `branch_rules`, `release_policy`, `release_line`, `placement_policy`. |
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
| `event_type` | text | нет | Имя события `project.*`. |
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

- `Project` владеет `RepositoryBinding`, `ServicesPolicy`, `ServiceDescriptor`, `DocumentationSource`, `BranchRules`, `ReleasePolicy`, `ReleaseLine`, `PlacementPolicy`, `PolicyOverride`.
- `ServicesPolicy` владеет набором `ServiceDescriptor`, полученным из проверенной версии `services.yaml`.
- `validated_payload` хранится как нормализованный JSON по модели политики `services.yaml`; сырой YAML остаётся в Git у провайдера.
- `ServiceDescriptor` считается активным только внутри последней политики `valid + synced/overridden`. Импорт невалидной или неуспешной проекции не переводит предыдущие descriptors в `stale`.
- Внутри БД `project-catalog` допустимы обычные внешние ключи между своими таблицами.
- Ссылки на организации, кластеры, роли, агентные процессы и provider-native сущности хранятся как внешние идентификаторы без SQL-связей с чужими БД.

## Индексы и запросы

| Запрос | Индексы |
|---|---|
| Список проектов организации | `(organization_id, status, slug)` |
| Список репозиториев проекта | `(project_id, status, provider, provider_owner, provider_name)` |
| Поиск репозитория по provider identity | `(provider, provider_owner, provider_name)` unique для активной привязки |
| Сервисы проекта | `(project_id, status, service_key)` unique для активного сервиса |
| Сервисы репозитория | `(repository_id, status, service_key)` |
| Актуальная проекция `services.yaml` | `(project_id, projection_status, policy_version)` |
| Сверка политики по источнику | `(source_repository_id, source_path, source_commit_sha, content_hash)` |
| Источники документации для рабочего контура | `(project_id, scope_type, scope_id, status)` |
| Активные правила веток | `(project_id, repository_id, status)` |
| Активные релизные политики | `(project_id, status)` |
| Релизные линии проекта или политики | `(project_id, release_policy_id, status)` |
| Активные переопределения | `(project_id, target_type, status, expires_at)` |
| Непубликованные события | `(published_at, occurred_at)` where `published_at is null` |
| Идемпотентный след команд | `(command_id)` unique |

## Политика хранения данных

- Архивные проекты и репозитории не удаляются физически в MVP, чтобы сохранить аудит и связи с provider-native артефактами.
- Старые версии `ServicesPolicy` хранятся как история проверенных проекций политики, управляемой через Git, с ограничением срока хранения, если содержимое станет большим.
- Штатная декларативная политика меняется через PR с правкой `services.yaml`; прямые `PolicyOverride` не заменяют источник намерения в Git и должны иметь срок действия.
- Сырые секреты, токены провайдера и содержимое приватных файлов не хранятся в домене.
- Иконки проектов и репозиториев хранятся как объекты в бакете; `project-catalog` хранит только `icon_object_uri` и не отвечает за загрузку, преобразование и выдачу бинарных изображений.

## Апрув

- request_id: `owner-2026-05-05-wave8-project-catalog-kickoff`
- Решение: approved
- Комментарий: модель данных домена проектов и репозиториев согласована как целевое состояние стартового среза.
