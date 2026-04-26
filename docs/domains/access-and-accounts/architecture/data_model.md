---
doc_id: DM-CK8S-AAC-0001
type: data-model
title: kodex — модель данных домена доступа и аккаунтов
status: active
owner_role: SA
created_at: 2026-04-26
updated_at: 2026-04-26
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

- Ключевые сущности: `Organization`, `User`, `UserIdentity`, `AllowlistEntry`, `Group`, `Membership`, `AccessRule`, `AccessDecisionAudit`, `ExternalAccount`, `ExternalAccountBinding`, `SecretBindingRef`.
- Основные связи: пользователь входит в организации и группы через типизированное членство, правила доступа действуют по области применения, внешний аккаунт связан с организацией, проектом, репозиторием или ролью через привязку политики.
- Риски миграций: нельзя зашить единственную организацию, хранить сырые секреты, смешать зеркало провайдера с политикой аккаунта и потерять объяснимость явного запрета.

## Общие инварианты

- Каждый агрегат имеет `id`, `version`, `created_at`, `updated_at`.
- Команды изменения используют ожидаемую версию или идемпотентный ключ.
- Сырые секреты не хранятся в PostgreSQL.
- Email хранится нормализованно для поиска и в маскированном виде для аудита там, где полный email не нужен.
- Связи на проекты, репозитории, роли и другие домены хранятся как внешние идентификаторы без `FOREIGN KEY` в чужие БД.

## Сущности

### `Organization`

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | UUID | no | Идентификатор организации. |
| `kind` | enum | no | `owner`, `client`, `contractor`, `saas`. |
| `slug` | string | no | Уникальный человекочитаемый ключ. |
| `display_name` | string | no | Название на выбранной локали. |
| `status` | enum | no | `active`, `pending`, `suspended`, `archived`. |
| `parent_organization_id` | UUID | yes | Для будущей иерархии. |
| `version` | int64 | no | Конкурентные изменения. |

Инварианты:
- в установке должна быть ровно одна активная организация-владелец;
- клиентские организации и организации внешних исполнителей не получают прав на платформенный контур без явных правил;
- архивирование организации публикует событие для других сервисов.

### `User`

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | UUID | no | Платформенный пользователь. |
| `primary_email` | string | no | Нормализованный email. |
| `display_name` | string | yes | Имя из IdP или ручной настройки. |
| `status` | enum | no | `active`, `pending`, `blocked`, `disabled`. |
| `locale` | string | yes | Предпочтительная локаль UI. |
| `version` | int64 | no | Конкурентные изменения. |

Инварианты:
- самостоятельная регистрация запрещена;
- пользователь может иметь несколько внешних идентичностей;
- пользователи в состояниях `blocked` и `disabled` не проходят `CheckAccess`.

### `UserIdentity`

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | UUID | no | Идентификатор связи. |
| `user_id` | UUID | no | Ссылка на `User` внутри БД домена. |
| `provider` | enum | no | `keycloak`, `github`, `gitlab`, другое. |
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

### `Group`

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | UUID | no | Идентификатор группы. |
| `scope_type` | enum | no | `global`, `organization`. |
| `scope_id` | UUID | yes | Для организационной области. |
| `slug` | string | no | Ключ внутри области. |
| `display_name` | string | no | Название. |
| `status` | enum | no | `active`, `disabled`, `archived`. |
| `version` | int64 | no | Конкурентные изменения. |

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

### `AccessRule`

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | UUID | no | Идентификатор правила. |
| `effect` | enum | no | `allow`, `deny`. |
| `subject_type` | enum | no | `user`, `group`, `organization`, `external_account`, `agent_role`. |
| `subject_id` | UUID | no | Идентификатор субъекта или внешняя ссылка для роли. |
| `action` | string | no | Каноническое действие. |
| `resource_type` | string | no | Тип ресурса: `project`, `repository`, `package`, `runtime`, другое. |
| `resource_id` | string | yes | Внешний идентификатор ресурса. |
| `scope_type` | enum | no | `global`, `organization`, `project`, `repository`. |
| `scope_id` | string | yes | Внешний идентификатор области. |
| `priority` | int | no | Для явных исключений. |
| `status` | enum | no | `active`, `disabled`. |
| `version` | int64 | no | Конкурентные изменения. |

Инвариант: явный запрет побеждает разрешение, если оба правила применимы к одному действию и ресурсу.

### `ExternalAccount`

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | UUID | no | Платформенный идентификатор внешнего аккаунта. |
| `provider` | enum/string | no | `github`, `gitlab`, `openai`, `telegram`, другое. |
| `account_type` | enum | no | `user`, `bot`, `service`, `integration`. |
| `display_name` | string | no | Название для оператора. |
| `owner_scope_type` | enum | no | `global`, `organization`, `project`, `repository`. |
| `owner_scope_id` | string | yes | Внешний идентификатор области владения. |
| `status` | enum | no | `active`, `pending`, `needs_reauth`, `limited`, `blocked`, `disabled`. |
| `secret_binding_ref_id` | UUID | yes | Ссылка на метаданные секрета. |
| `version` | int64 | no | Конкурентные изменения. |

### `ExternalAccountBinding`

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | UUID | no | Идентификатор привязки. |
| `external_account_id` | UUID | no | Внешний аккаунт. |
| `usage_scope_type` | enum | no | `organization`, `project`, `repository`, `agent_role`, `package`. |
| `usage_scope_id` | string | no | Внешний идентификатор области использования. |
| `allowed_actions` | string[] | no | Допустимые действия. |
| `status` | enum | no | `active`, `disabled`. |

### `SecretBindingRef`

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | UUID | no | Идентификатор ссылки. |
| `store_type` | enum | no | `vault`, `kubernetes_secret`, будущие типы. |
| `store_ref` | string | no | Путь или имя секрета без значения. |
| `value_fingerprint` | string | yes | Нераскрывающий отпечаток для диагностики ротации. |
| `rotated_at` | timestamp | yes | Последняя известная ротация. |

### `AccessDecisionAudit`

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | UUID | no | Идентификатор записи. |
| `subject_type` | string | no | Субъект решения. |
| `subject_id` | string | no | Идентификатор субъекта. |
| `action` | string | no | Действие. |
| `resource_type` | string | no | Тип ресурса. |
| `resource_id` | string | yes | Идентификатор ресурса. |
| `decision` | enum | no | `allow`, `deny`, `pending`. |
| `reason_code` | string | no | Машинно читаемая причина. |
| `policy_version` | int64 | no | Версия политики. |
| `explanation` | jsonb | no | Объяснение без секретов. |
| `created_at` | timestamp | no | Время решения. |

## Связи

- `User` 1:N `UserIdentity`.
- `User` M:N `Organization` через `Membership`.
- `User` M:N `Group` через `Membership`.
- `Group` M:N `Group` через `Membership`, если понадобится вложенность групп.
- `ExternalAccount` N:1 `SecretBindingRef`.
- `ExternalAccount` 1:N `ExternalAccountBinding`.
- `AccessRule` ссылается на субъекты и ресурсы через типизированные идентификаторы.
- `AccessDecisionAudit` не является источником прав, а только следом решения.

## Индексы и запросы

| Запрос | Индексы |
|---|---|
| Создание или связывание профиля по email и subject провайдера | `UserIdentity(provider, subject)`, `AllowlistEntry(match_type, value)`, `User(primary_email)`. |
| Граф членства пользователя | `Membership(subject_type, subject_id, status)`, `Membership(target_type, target_id, status)`. |
| Проверка доступа | `AccessRule(subject_type, subject_id, action, resource_type, status)`, `AccessRule(scope_type, scope_id, action, status)`. |
| Внешние аккаунты по области | `ExternalAccount(owner_scope_type, owner_scope_id, status)`, `ExternalAccountBinding(usage_scope_type, usage_scope_id, status)`. |
| Аудит решений | `AccessDecisionAudit(subject_type, subject_id, created_at)`, `AccessDecisionAudit(resource_type, resource_id, created_at)`. |

## Политика хранения данных

- `AccessDecisionAudit` хранится по политике аудита платформы; удаление без архивной политики запрещено.
- Сырые секреты не попадают в БД.
- Записи входа и отказов можно агрегировать, но аудит решений доступа должен сохранять объяснение.
- Персональные данные минимизируются: email нужен для входа и поиска, но не должен копироваться в чужие домены без необходимости.

## Апрув

- request_id: `owner-2026-04-26-wave6-4-access-domain`
- Решение: approved
- Комментарий: модель данных домена доступа согласована как целевое состояние.
