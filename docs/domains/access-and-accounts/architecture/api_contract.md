---
doc_id: API-CK8S-AAC-0001
type: api-contract
title: kodex — API-контракт домена доступа и аккаунтов
status: active
owner_role: SA
created_at: 2026-04-26
updated_at: 2026-04-27
related_issues: [599, 600, 601, 602]
related_prs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-04-26-wave6-4-access-domain"
  approved_by: "ai-da-stas"
  approved_at: 2026-04-26
---

# Обзор API-контракта: access-manager

## TL;DR

- Тип API: gRPC для межсервисных команд и чтений, HTTP через `api-gateway` для пользовательского интерфейса, доменные события через outbox.
- Аутентификация: сервисная идентичность внутри платформы, пользовательская сессия через `api-gateway`, MCP-вызовы через `platform-mcp-server`.
- Версионирование: контракты версионируются по proto/OpenAPI/AsyncAPI после создания целевых спецификаций; события имеют `schema_version`.
- Основные операции: создание или связывание пользователя, управление организациями и группами, членство, внешние аккаунты, проверка доступа и объяснение аудита.

## Источники спецификаций

Машинно-проверяемые спецификации access-manager созданы сразу как стабильный контракт `v1`:
- gRPC proto: `proto/kodex/access_accounts/v1/access_manager.proto`;
- OpenAPI для пользовательского интерфейса и внешних HTTP-контрактов: `specs/openapi/access-manager.v1.yaml`;
- AsyncAPI для событий: `specs/asyncapi/access-manager.v1.yaml`.

Каталог `docs/**` хранит описание решений, а `proto/**` и `specs/**` являются источником истины для транспорта, проверки, генерации и клиентского кода. Если документация описывает обязательный API-путь, стабильный транспортный контракт `v1` должен покрывать его полностью; частичные предварительные контракты допустимы только с явным суффиксом версии и отдельным статусом.

## Версионный статус

| Контракт | Файл | Статус | Объём |
|---|---|---|---|
| gRPC | `proto/kodex/access_accounts/v1/access_manager.proto` | стабильный `v1` | Все команды и чтения из раздела ниже. |
| HTTP | `specs/openapi/access-manager.v1.yaml` | стабильный `v1` | Все пользовательские, администраторские и операторские HTTP-сценарии домена. |
| События | `specs/asyncapi/access-manager.v1.yaml` | стабильный `v1` | Все доменные события из таблицы минимальных тел. |

## Команды и чтения

| Операция | Тип | Доступ | Идемпотентность | Назначение |
|---|---|---|---|---|
| `BootstrapUserFromIdentity` | команда | пользовательская сессия | обязательно | Создать или связать пользователя после SSO/OIDC и allowlist. |
| `SetUserStatus` | команда | администратор | обязательно | Изменить статус пользователя. |
| `CreateOrganization` | команда | администратор | обязательно | Создать организацию. |
| `UpdateOrganization` | команда | администратор | обязательно | Изменить безопасные поля организации. |
| `SuspendOrganization` | команда | администратор + политика | обязательно | Приостановить организацию, если это не организация-владелец. |
| `ArchiveOrganization` | команда | администратор + политика | обязательно | Архивировать организацию. |
| `CreateGroup` | команда | администратор | обязательно | Создать глобальную или организационную группу. |
| `UpdateGroup` | команда | администратор | обязательно | Изменить группу или её родительскую группу. |
| `DisableGroup` | команда | администратор | обязательно | Отключить группу без удаления истории. |
| `SetMembership` | команда | администратор | обязательно | Создать, изменить или отключить членство. |
| `PutAllowlistEntry` | команда | администратор | обязательно | Создать или изменить allowlist. |
| `DisableAllowlistEntry` | команда | администратор | обязательно | Отключить запись allowlist без потери истории. |
| `RegisterExternalProvider` | команда | администратор | обязательно | Завести поставщика внешних аккаунтов и его визуальные метаданные. |
| `UpdateExternalProvider` | команда | администратор | обязательно | Изменить поставщика внешних аккаунтов. |
| `RegisterExternalAccount` | команда | администратор + политика | обязательно | Завести внешний аккаунт как субъект политики. |
| `UpdateExternalAccountStatus` | команда | администратор/service | обязательно | Изменить статус внешнего аккаунта. |
| `BindExternalAccount` | команда | администратор + политика | обязательно | Разрешить использование аккаунта в области. |
| `DisableExternalAccountBinding` | команда | администратор + политика | обязательно | Отключить привязку внешнего аккаунта к области использования. |
| `PutAccessAction` | команда | администратор | обязательно | Завести или обновить действие из каталога прав. |
| `PutAccessRule` | команда | администратор + политика | обязательно | Создать или изменить правило доступа. |
| `DisableAccessRule` | команда | администратор + политика | обязательно | Отключить правило доступа. |
| `ResolveExternalAccountUsage` | чтение | сервис | нет | Проверить, можно ли использовать аккаунт для операции, и вернуть `secret_ref`. |
| `CheckAccess` | чтение/решение | сервис/MCP/пользовательский интерфейс | необязательный ключ аудита | Вычислить доступ для субъекта, действия и ресурса. |
| `ExplainAccess` | чтение | администратор/оператор | нет | Получить объяснение решения доступа. |
| `ListMembershipGraph` | чтение | администратор/оператор | нет | Получить граф членства для пользовательского интерфейса. |
| `ListPendingAccess` | чтение | администратор/оператор | нет | Получить входы и действия в состояниях `pending` и `blocked`. |

## Модель команд

Каждая команда изменения должна принимать:
- `command_id` или `idempotency_key`;
- `expected_version`, если команда меняет существующий агрегат;
- `actor`;
- `reason`;
- `request_context`;
- минимальное тело запроса без секретов и лишних полей; персональные данные передаются только там, где без них команда не имеет смысла.

Если `expected_version` устарела, сервис возвращает конфликт и не применяет изменение.

## Модель ошибок

| Код | Когда используется |
|---|---|
| `UNAUTHENTICATED` | Не передана или не проверена идентичность вызывающей стороны. |
| `PERMISSION_DENIED` | Вызывающая сторона не имеет права на действие. |
| `FAILED_PRECONDITION` | Нарушен доменный инвариант или статус не допускает операцию. |
| `ALREADY_EXISTS` | Создаваемая сущность уже существует в той же области. |
| `NOT_FOUND` | Сущность не найдена или не видна вызывающей стороне. |
| `ABORTED` | Конфликт версии агрегата. |
| `RESOURCE_EXHAUSTED` | Достигнут лимит политики или временная блокировка. |
| `INVALID_ARGUMENT` | Неверный формат команды. |

## Контракты событий

| Событие | Минимальное тело |
|---|---|
| `access.organization.created` | `event_id`, `organization_id`, `kind`, `status`, `version`, `occurred_at`. |
| `access.organization.updated` | `event_id`, `organization_id`, `version`, `occurred_at`. |
| `access.organization.suspended` | `event_id`, `organization_id`, `reason_code`, `version`, `occurred_at`. |
| `access.organization.archived` | `event_id`, `organization_id`, `reason_code`, `version`, `occurred_at`. |
| `access.user.created` | `event_id`, `user_id`, `status`, `version`, `occurred_at`. |
| `access.user.updated` | `event_id`, `user_id`, `version`, `occurred_at`. |
| `access.user.identity_linked` | `event_id`, `user_id`, `identity_id`, `identity_provider`, `version`, `occurred_at`. |
| `access.user.status_changed` | `event_id`, `user_id`, `old_status`, `new_status`, `reason_code`, `version`, `occurred_at`. |
| `access.allowlist_entry.created` | `event_id`, `allowlist_entry_id`, `match_type`, `version`, `occurred_at`. |
| `access.allowlist_entry.updated` | `event_id`, `allowlist_entry_id`, `version`, `occurred_at`. |
| `access.allowlist_entry.disabled` | `event_id`, `allowlist_entry_id`, `reason_code`, `version`, `occurred_at`. |
| `access.group.created` | `event_id`, `group_id`, `scope_type`, `version`, `occurred_at`. |
| `access.group.updated` | `event_id`, `group_id`, `version`, `occurred_at`. |
| `access.group.disabled` | `event_id`, `group_id`, `reason_code`, `version`, `occurred_at`. |
| `access.membership.created` | `event_id`, `membership_id`, `subject_type`, `subject_id`, `target_type`, `target_id`, `version`, `occurred_at`. |
| `access.membership.updated` | `event_id`, `membership_id`, `version`, `occurred_at`. |
| `access.membership.disabled` | `event_id`, `membership_id`, `reason_code`, `version`, `occurred_at`. |
| `access.external_provider.created` | `event_id`, `external_provider_id`, `slug`, `version`, `occurred_at`. |
| `access.external_provider.updated` | `event_id`, `external_provider_id`, `version`, `occurred_at`. |
| `access.external_provider.disabled` | `event_id`, `external_provider_id`, `reason_code`, `version`, `occurred_at`. |
| `access.external_account.created` | `event_id`, `external_account_id`, `external_provider_id`, `account_type`, `version`, `occurred_at`. |
| `access.external_account.updated` | `event_id`, `external_account_id`, `version`, `occurred_at`. |
| `access.external_account.status_changed` | `event_id`, `external_account_id`, `old_status`, `new_status`, `reason_code`, `version`, `occurred_at`. |
| `access.external_account.secret_ref_changed` | `event_id`, `external_account_id`, `secret_binding_ref_id`, `version`, `occurred_at`. |
| `access.external_account_binding.created` | `event_id`, `external_account_binding_id`, `external_account_id`, `usage_scope_type`, `usage_scope_id`, `version`, `occurred_at`. |
| `access.external_account_binding.updated` | `event_id`, `external_account_binding_id`, `version`, `occurred_at`. |
| `access.external_account_binding.disabled` | `event_id`, `external_account_binding_id`, `reason_code`, `version`, `occurred_at`. |
| `access.secret_binding_ref.created` | `event_id`, `secret_binding_ref_id`, `store_type`, `version`, `occurred_at`. |
| `access.secret_binding_ref.rotated` | `event_id`, `secret_binding_ref_id`, `version`, `occurred_at`. |
| `access.secret_binding_ref.disabled` | `event_id`, `secret_binding_ref_id`, `reason_code`, `version`, `occurred_at`. |
| `access.access_action.created` | `event_id`, `access_action_id`, `action_key`, `version`, `occurred_at`. |
| `access.access_action.updated` | `event_id`, `access_action_id`, `version`, `occurred_at`. |
| `access.access_action.disabled` | `event_id`, `access_action_id`, `reason_code`, `version`, `occurred_at`. |
| `access.access_rule.created` | `event_id`, `access_rule_id`, `effect`, `action_key`, `scope_type`, `version`, `occurred_at`. |
| `access.access_rule.updated` | `event_id`, `access_rule_id`, `version`, `occurred_at`. |
| `access.access_rule.disabled` | `event_id`, `access_rule_id`, `reason_code`, `version`, `occurred_at`. |
| `access.access_decision.recorded` | `event_id`, `access_decision_audit_id`, `subject_type`, `subject_id`, `action_key`, `decision`, `reason_code`, `occurred_at`. |

События фиксируют факт уже совершённого изменения и не являются командой другому сервису. Они близки к `created/updated/disabled/archived`, но не являются механическим CRUD по таблицам: имя события отражает доменный переход. В событие передаются идентификаторы, версия, тип перехода и машинный код причины; имена, email, секреты, токены, свободный текст и другие чувствительные данные не публикуются. Для высокочастотных решений `CheckAccess` канонический след хранится в `AccessDecisionAudit`; событие по решению публикуется только для критичных решений, если это требует политика аудита.

## Наблюдаемость

- Логи: `request_id`, `command_id`, `actor_id`, `operation`, `aggregate_id`, `decision`, без секретов, персональных данных, токенов, email, имён и лишнего тела запроса.
- Метрики: задержка операций, конфликты, запрещённые решения, пользователи в ожидании, заблокированные пользователи, статусы внешних аккаунтов.
- Трейсы: `api-gateway -> access-manager`, `platform-mcp-server -> access-manager`, `provider-hub -> access-manager`.
- Аудит: отдельная доменная запись `AccessDecisionAudit` для решений доступа и административных изменений.

## Апрув

- request_id: `owner-2026-04-26-wave6-4-access-domain`
- Решение: approved
- Комментарий: API-контракт домена доступа согласован как целевое состояние.
