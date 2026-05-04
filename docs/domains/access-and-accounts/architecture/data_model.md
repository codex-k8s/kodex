---
doc_id: DM-CK8S-AAC-0001
type: data-model
title: kodex — модель данных домена доступа и аккаунтов
status: active
owner_role: SA
created_at: 2026-04-26
updated_at: 2026-05-04
related_issues: [599, 600, 601, 602]
related_prs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-04-26-wave6-4-access-domain"
  approved_by: "ai-da-stas"
  approved_at: 2026-04-26
---

# Модель данных: домен доступа и аккаунтов

## TL;DR

- Ключевые сущности: `Organization`, `User`, `UserIdentity`, `AllowlistEntry`, `Group`, `Membership`, `AccessAction`, `AccessRule`, `AccessDecisionAudit`, `ExternalProvider`, `ExternalAccount`, `ExternalAccountBinding`, `SecretBindingRef`, `CommandResult`, `OutboxEvent`.
- Основные связи: пользователь входит в организации и группы через типизированное членство, правила доступа действуют по области применения, внешний аккаунт связан с организацией, проектом, репозиторием или ролью через привязку политики.
- Риски миграций: нельзя зашить единственную организацию, хранить сырые секреты, смешать зеркало провайдера с политикой аккаунта и потерять объяснимость явного запрета.

## Общие инварианты

- Каждый агрегат имеет `id`, `version`, `created_at`, `updated_at`.
- Команды изменения используют ожидаемую версию или идемпотентный ключ; команды создания без естественного бизнес-ключа сохраняют результат в `CommandResult`.
- Сырые секреты не хранятся в PostgreSQL.
- Email хранится нормализованно для поиска и в маскированном виде для аудита там, где полный email не нужен.
- Связи на проекты, репозитории, роли и другие домены хранятся как внешние идентификаторы без `FOREIGN KEY` в чужие БД.
- `FOREIGN KEY` допустимы внутри БД `access-manager`, например от внешнего аккаунта к поставщику внешних аккаунтов.
- Сущности, которые показываются в пользовательском интерфейсе как карточки или строки каталога, имеют необязательную ссылку на картинку в S3-compatible объектном хранилище: `image_asset_ref` или `avatar_asset_ref`.

## Сущности

### `Organization`

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | UUID | no | Идентификатор организации. |
| `kind` | enum | no | `owner`, `client`, `contractor`, `saas`, `saas_client`, `saas_contractor`. |
| `slug` | string | no | Уникальный человекочитаемый ключ. |
| `display_name` | string | no | Название на выбранной локали. |
| `image_asset_ref` | string | yes | Ссылка на логотип или картинку организации в объектном хранилище. |
| `status` | enum | no | `active`, `pending`, `suspended`, `archived`. |
| `parent_organization_id` | UUID | yes | Для будущей иерархии. |
| `version` | int64 | no | Конкурентные изменения. |

Инварианты:
- в установке должна быть ровно одна активная организация-владелец;
- организация-владелец создаётся и остаётся только в статусе `active`;
- клиентские организации и организации внешних исполнителей не получают прав на платформенный контур без явных правил;
- архивирование организации публикует событие для других сервисов.

### `User`

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | UUID | no | Платформенный пользователь. |
| `primary_email` | string | no | Нормализованный email. |
| `display_name` | string | yes | Имя из IdP или ручной настройки. |
| `avatar_asset_ref` | string | yes | Ссылка на аватар в объектном хранилище. |
| `status` | enum | no | `active`, `pending`, `blocked`, `disabled`. |
| `locale` | string | yes | Предпочтительная локаль пользовательского интерфейса. |
| `version` | int64 | no | Конкурентные изменения. |

Инварианты:
- самостоятельная регистрация запрещена;
- пользователь может иметь несколько внешних идентичностей;
- пользователи в состоянии `pending` получают решение `pending`, а в состояниях `blocked` и `disabled` не проходят `CheckAccess`.

### `UserIdentity`

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | UUID | no | Идентификатор связи. |
| `user_id` | UUID | no | Ссылка на `User` внутри БД домена. |
| `provider` | enum | no | `keycloak`, `github`, `gitlab`, `google`, другое. |
| `subject` | string | no | Внешний subject. |
| `email_at_login` | string | no | Email на момент входа. |
| `last_login_at` | timestamp | yes | Последний успешный вход. |

### `AllowlistEntry`

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | UUID | no | Идентификатор записи. |
| `match_type` | enum | no | `email`, `domain`. |
| `value` | string | no | Нормализованное значение. |
| `organization_id` | UUID | yes | Организация первичного допуска. |
| `default_status` | enum | no | `active` или `pending`. |
| `status` | enum | no | `active`, `disabled`. |
| `version` | int64 | no | Конкурентные изменения. |

Инварианты:
- `disabled` запись allowlist является явным отказом для совпавшего email или домена;
- точное совпадение по email имеет приоритет над доменным совпадением;
- по `disabled` записи пользователь не создаётся даже в статусе `pending`.

### `Group`

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | UUID | no | Идентификатор группы. |
| `scope_type` | enum | no | `global`, `organization`. |
| `scope_id` | UUID | yes | Для организационной области. |
| `slug` | string | no | Ключ внутри области. |
| `display_name` | string | no | Название. |
| `parent_group_id` | UUID | yes | Родительская группа внутри той же области. |
| `image_asset_ref` | string | yes | Ссылка на иконку или картинку группы в объектном хранилище. |
| `status` | enum | no | `active`, `disabled`, `archived`. |
| `version` | int64 | no | Конкурентные изменения. |

Инварианты:
- родительская группа должна находиться в той же области, что и дочерняя;
- `slug` уникален внутри области: отдельно для `global` и отдельно внутри каждой организации;
- иерархия групп не должна содержать циклов.

### `Membership`

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | UUID | no | Идентификатор членства. |
| `subject_type` | enum | no | `user`, `group`, `external_account`, будущие типы субъектов. |
| `subject_id` | UUID | no | Ссылка внутри домена доступа. |
| `target_type` | enum | no | `organization`, `group`, будущие области. |
| `target_id` | UUID | no | Целевая сущность внутри домена доступа. |
| `role_hint` | string | yes | Человекочитаемая роль, не заменяет `AccessRule`. |
| `status` | enum | no | `active`, `pending`, `blocked`, `disabled`. |
| `source` | enum | no | `manual`, `bootstrap`, `sync`, `system`. |
| `version` | int64 | no | Конкурентные изменения. |

### `AccessAction`

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | UUID | no | Идентификатор действия. |
| `key` | string | no | Канонический ключ действия, например `project.read`, `provider.issue.write`, `runtime.job.start`. |
| `display_name` | string | no | Название на выбранной локали. |
| `description` | string | yes | Описание для администратора. |
| `resource_type` | string | no | Базовый тип ресурса, к которому относится действие. |
| `status` | enum | no | `active`, `disabled`. |
| `version` | int64 | no | Конкурентные изменения. |

Инвариант: действия не должны быть PostgreSQL enum, потому что новые домены, пакеты и интеграции могут добавлять свои действия без миграции общей enum-схемы. Типизация обеспечивается каталогом `AccessAction`, проверками контракта и константами в коде.

### `AccessRule`

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | UUID | no | Идентификатор правила. |
| `effect` | enum | no | `allow`, `deny`. |
| `subject_type` | enum | no | `user`, `group`, `organization`, `external_account`, `agent`, `agent_role`, `flow`, `package`. |
| `subject_id` | string | no | UUID для субъектов домена доступа или внешний идентификатор для роли, агента, flow и пакета. |
| `action_key` | string | no | Канонический ключ из каталога `AccessAction`. |
| `resource_type` | string | no | Тип ресурса: `project`, `repository`, `package`, `runtime`, другое. |
| `resource_id` | string | yes | Внешний идентификатор ресурса. |
| `scope_type` | enum | no | `global`, `organization`, `project`, `repository`. |
| `scope_id` | string | yes | Внешний идентификатор области; для `global` всегда пустой. |
| `priority` | int | no | Для явных исключений. |
| `status` | enum | no | `active`, `disabled`. |
| `version` | int64 | no | Конкурентные изменения. |

Инварианты:
- явный запрет побеждает разрешение, если оба правила применимы к одному действию и ресурсу;
- правило с `scope_type=global` и пустым `scope_id` применяется ко всем областям;
- правило можно создать только для активного `AccessAction`.

### `ExternalProvider`

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | UUID | no | Идентификатор поставщика внешних аккаунтов. |
| `slug` | string | no | Стабильный ключ, например `github`, `gitlab`, `openai`, `telegram`. |
| `provider_kind` | enum | no | `repository`, `identity`, `model`, `messaging`, `payments`, `other`. |
| `display_name` | string | no | Название для пользовательского интерфейса. |
| `icon_asset_ref` | string | yes | Ссылка на иконку в объектном хранилище. |
| `status` | enum | no | `active`, `disabled`. |
| `version` | int64 | no | Конкурентные изменения. |

`ExternalProvider` — это каталог поставщиков внешних аккаунтов для политики доступа. Он не заменяет `provider-hub`: runtime-обработчики, webhook, лимиты и операции провайдера остаются в `provider-hub` и ссылаются на этот каталог по идентификатору или `slug`.

### `ExternalAccount`

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | UUID | no | Платформенный идентификатор внешнего аккаунта. |
| `external_provider_id` | UUID | no | `FOREIGN KEY` на `ExternalProvider` внутри БД `access-manager`. |
| `account_type` | enum | no | `user`, `bot`, `service`, `integration`. |
| `display_name` | string | no | Название для оператора. |
| `image_asset_ref` | string | yes | Ссылка на аватар или картинку аккаунта в объектном хранилище. |
| `owner_scope_type` | enum | no | `global`, `organization`, `project`, `repository`, `user`, `group`, `agent`, `agent_role`, `flow`, `package`. |
| `owner_scope_id` | string | yes | Внешний идентификатор области владения. |
| `status` | enum | no | `active`, `pending`, `needs_reauth`, `limited`, `blocked`, `disabled`. |
| `secret_binding_ref_id` | UUID | yes | Ссылка на метаданные секрета. |
| `version` | int64 | no | Конкурентные изменения. |

### `ExternalAccountBinding`

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | UUID | no | Идентификатор привязки. |
| `external_account_id` | UUID | no | Внешний аккаунт. |
| `usage_scope_type` | enum | no | `organization`, `project`, `repository`, `user`, `group`, `agent`, `agent_role`, `flow`, `stage`, `package`. |
| `usage_scope_id` | string | no | Внешний идентификатор области использования. |
| `allowed_action_keys` | string[] | no | Допустимые действия из каталога `AccessAction`. |
| `status` | enum | no | `active`, `disabled`. |
| `version` | int64 | no | Конкурентные изменения. |

### `SecretBindingRef`

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | UUID | no | Идентификатор ссылки. |
| `store_type` | enum | no | `vault`, `kubernetes_secret`, будущие типы. |
| `store_ref` | string | no | Путь или имя секрета без значения. |
| `value_fingerprint` | string | yes | Нераскрывающий отпечаток для диагностики ротации. |
| `rotated_at` | timestamp | yes | Последняя известная ротация. |
| `version` | int64 | no | Конкурентные изменения. |

`owner_scope_type` показывает, кто владеет внешним аккаунтом и отвечает за его секрет. `ExternalAccountBinding` показывает, кому разрешено использовать аккаунт. Это позволяет завести личный аккаунт пользователя, аккаунт отдельного агента, аккаунт группы, аккаунт роли или аккаунт конкретного flow без изменения структуры таблиц.

### `AccessDecisionAudit`

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | UUID | no | Идентификатор записи. |
| `subject_type` | string | no | Субъект решения. |
| `subject_id` | string | no | Идентификатор субъекта. |
| `action_key` | string | no | Ключ действия из каталога `AccessAction`. |
| `resource_type` | string | no | Тип ресурса. |
| `resource_id` | string | yes | Идентификатор ресурса. |
| `scope_type` | string | no | Область, где вычислялась политика. |
| `scope_id` | string | yes | Идентификатор области; глобальная область использует пустое значение. |
| `request_context` | jsonb | no | Безопасный контекст запроса: источник, trace, session и хеш IP без токенов, email, имён и секретов. |
| `decision` | enum | no | `allow`, `deny`, `pending`. |
| `reason_code` | string | no | Машинно читаемая причина. |
| `policy_version` | int64 | no | Версия политики. |
| `explanation` | jsonb | no | Объяснение без секретов и чувствительных данных. |
| `created_at` | timestamp | no | Время решения. |

### `CommandResult`

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `key` | string | no | Внутренний ключ результата команды. |
| `command_id` | UUID | yes | Стабильный идентификатор команды, если его передал вызывающий контур. |
| `idempotency_key` | string | yes | Идемпотентный ключ, если `command_id` не передан. |
| `operation` | string | no | Канонический путь операции, например `domain.Service.CreateOrganization`. |
| `aggregate_type` | string | no | Тип созданного агрегата. |
| `aggregate_id` | UUID | no | Идентификатор созданного агрегата. |
| `created_at` | timestamp | no | Время фиксации команды. |

Инварианты:
- запись создаётся в одной транзакции с агрегатом и outbox-событием;
- повтор той же команды возвращает уже созданный агрегат и не создаёт второе бизнес-изменение;
- если один и тот же `command_id` или `idempotency_key` используется для другой операции, сервис возвращает конфликт;
- таблица не заменяет естественные бизнес-ключи для команд обновления или создания по бизнес-ключу, а закрывает команды создания без устойчивого provider-native идентификатора.

### `OutboxEvent`

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | UUID | no | Идентификатор события и ключ дедупликации у потребителей. |
| `event_type` | string | no | Тип события `access.*` из AsyncAPI. |
| `schema_version` | int | no | Версия схемы события. |
| `aggregate_type` | string | no | Тип агрегата-владельца события. |
| `aggregate_id` | UUID | no | Идентификатор агрегата-владельца события. |
| `payload` | jsonb | no | Полезная нагрузка события без секретов и лишних персональных данных. |
| `occurred_at` | timestamp | no | Время доменного изменения. |
| `published_at` | timestamp | yes | Время успешной записи события в штатный канал публикации; для MVP это общий PostgreSQL-журнал событий. |
| `attempt_count` | int | no | Номер попытки доставки; увеличивается при захвате события доставщиком. |
| `next_attempt_at` | timestamp | no | Время, раньше которого событие не забирается после временной ошибки. |
| `locked_until` | timestamp | yes | Короткая аренда записи конкретным экземпляром доставщика. |
| `failed_permanently_at` | timestamp | yes | Время постоянного сбоя, после которого автоматический повтор не выполняется. |
| `failure_kind` | string | no | Тип последнего сбоя: пустое значение, `transient` или `permanent`. |
| `last_error` | string | no | Короткий текст последней ошибки для диагностики без полного лога. |

Инварианты:
- запись создаётся в одной транзакции с изменением агрегата и, если нужно, `CommandResult`;
- доставщик забирает событие только если оно не опубликовано, не помечено постоянным сбоем, наступило `next_attempt_at` и истекла предыдущая аренда;
- отметки успеха и ошибки проверяют текущий `attempt_count`, чтобы поздний обработчик не менял уже повторно забранную запись;
- временный сбой переводит событие в повтор, постоянный сбой выводит событие из автоматической доставки до операторского разбора;
- потребители должны дедуплицировать доставку по `id`;
- штатный канал публикации `postgres-event-log` пишет событие в `platform_event_log`, где `id` outbox-события становится `event_id` общего журнала.

## Связи

- `User` 1:N `UserIdentity`.
- `User` M:N `Organization` через `Membership`.
- `User` M:N `Group` через `Membership`.
- `Group` M:N `Group` через `Membership`, если понадобится вложенность групп.
- `ExternalProvider` 1:N `ExternalAccount`.
- `ExternalAccount` N:1 `SecretBindingRef`.
- `ExternalAccount` 1:N `ExternalAccountBinding`.
- `AccessAction` 1:N `AccessRule`.
- `AccessRule` ссылается на субъекты и ресурсы через типизированные идентификаторы.
- `AccessDecisionAudit` не является источником прав, а только следом решения.
- `CommandResult` ссылается на созданный агрегат по типу и идентификатору без межтабличного `FOREIGN KEY`, потому что хранит обобщённый результат разных команд создания.
- `OutboxEvent` ссылается на агрегат по типу и идентификатору без межтабличного `FOREIGN KEY`, потому что обслуживает события разных агрегатов домена.

## Индексы и запросы

| Запрос | Индексы |
|---|---|
| Создание или связывание профиля по email и subject провайдера | `UserIdentity(provider, subject)`, `AllowlistEntry(match_type, value)`, `User(primary_email)`. |
| Граф членства пользователя | `Membership(subject_type, subject_id, status)`, `Membership(target_type, target_id, status)`. |
| Повтор команды после сетевой ошибки | `CommandResult(command_id)`, `CommandResult(idempotency_key)`. |
| Проверка доступа | `AccessRule(subject_type, subject_id, action_key, resource_type, status)`, `AccessRule(scope_type, scope_id, action_key, status)`. |
| Внешние аккаунты по области | `ExternalAccount(owner_scope_type, owner_scope_id, status)`, `ExternalAccountBinding(usage_scope_type, usage_scope_id, status)`. |
| Внешние аккаунты по поставщику | `ExternalProvider(slug, status)`, `ExternalAccount(external_provider_id, status)`. |
| Аудит решений | `AccessDecisionAudit(subject_type, subject_id, created_at)`, `AccessDecisionAudit(resource_type, resource_id, created_at)`, `AccessDecisionAudit(action_key, created_at)`. |

## Политика хранения данных

- `AccessDecisionAudit` хранится по политике аудита платформы; удаление без архивной политики запрещено.
- Сырые секреты не попадают в БД.
- Записи входа и отказов можно агрегировать, но аудит решений доступа должен сохранять объяснение.
- Персональные данные минимизируются: email нужен для входа и поиска, но не должен копироваться в чужие домены без необходимости.

## Апрув

- request_id: `owner-2026-04-26-wave6-4-access-domain`
- Решение: approved
- Комментарий: модель данных домена доступа согласована как целевое состояние.
