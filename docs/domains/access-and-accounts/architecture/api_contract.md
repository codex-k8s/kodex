---
doc_id: API-CK8S-AAC-0001
type: api-contract
title: kodex — API-контракт домена доступа и аккаунтов
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

# Обзор API-контракта: access-manager

## TL;DR

- Тип API: gRPC для межсервисных команд и чтений, HTTP через `api-gateway` для пользовательского интерфейса, доменные события через outbox.
- Аутентификация: сервисная идентичность внутри платформы, пользовательская сессия через `api-gateway`, MCP-вызовы через `platform-mcp-server`.
- Версионирование: контракты версионируются по proto/OpenAPI после создания целевого `proto/**`; события имеют `schema_version`.
- Основные операции: создание или связывание пользователя, управление организациями и группами, членство, внешние аккаунты, проверка доступа и объяснение аудита.

## Источники спецификаций

До создания целевого `proto/**` этот документ является контрактным обзором. При реализации нужно создать:
- gRPC proto: будущий `proto/kodex/access/v1/access_manager.proto`;
- OpenAPI для UI: будущий `docs/domains/access-and-accounts/architecture/openapi.yaml` или общий каталог API;
- AsyncAPI для событий: будущий `docs/domains/access-and-accounts/architecture/asyncapi.yaml`.

## Команды и чтения

| Операция | Тип | Доступ | Идемпотентность | Назначение |
|---|---|---|---|---|
| `BootstrapUserFromIdentity` | команда | пользовательская сессия | обязательно | Создать или связать пользователя после SSO/OIDC и allowlist. |
| `SetUserStatus` | команда | администратор | обязательно | Изменить статус пользователя. |
| `CreateOrganization` | команда | администратор | обязательно | Создать организацию. |
| `ArchiveOrganization` | команда | администратор + политика | обязательно | Архивировать организацию. |
| `CreateGroup` | команда | администратор | обязательно | Создать глобальную или организационную группу. |
| `SetMembership` | команда | администратор | обязательно | Создать, изменить или отключить членство. |
| `PutAllowlistEntry` | команда | администратор | обязательно | Создать или изменить allowlist. |
| `RegisterExternalAccount` | команда | администратор + политика | обязательно | Завести внешний аккаунт как субъект политики. |
| `BindExternalAccount` | команда | администратор + политика | обязательно | Разрешить использование аккаунта в области. |
| `ResolveExternalAccountUsage` | чтение | сервис | нет | Проверить, можно ли использовать аккаунт для операции, и вернуть `secret_ref`. |
| `CheckAccess` | чтение/решение | сервис/MCP/UI | необязательный ключ аудита | Вычислить доступ для субъекта, действия и ресурса. |
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
- минимальное тело запроса без секретов.

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
| `access.organization.archived` | `event_id`, `organization_id`, `reason`, `version`, `occurred_at`. |
| `access.user.bootstrapped` | `event_id`, `user_id`, `identity_provider`, `status`, `organization_id`, `version`. |
| `access.user.status_changed` | `event_id`, `user_id`, `old_status`, `new_status`, `reason`, `version`. |
| `access.membership.changed` | `event_id`, `membership_id`, `subject`, `target`, `status`, `version`. |
| `access.external_account.changed` | `event_id`, `external_account_id`, `provider`, `account_type`, `status`, `version`. |
| `access.policy.changed` | `event_id`, `rule_id`, `effect`, `scope`, `version`. |

События фиксируют факт уже совершённого изменения и не являются командой другому сервису.

## Наблюдаемость

- Логи: `request_id`, `command_id`, `actor`, `operation`, `decision`, без секретов.
- Метрики: задержка операций, конфликты, запрещённые решения, пользователи в ожидании, заблокированные пользователи, статусы внешних аккаунтов.
- Трейсы: `api-gateway -> access-manager`, `platform-mcp-server -> access-manager`, `provider-hub -> access-manager`.
- Аудит: отдельная доменная запись `AccessDecisionAudit` для решений доступа и административных изменений.

## Апрув

- request_id: `owner-2026-04-26-wave6-4-access-domain`
- Решение: approved
- Комментарий: API-контракт домена доступа согласован как целевое состояние.
