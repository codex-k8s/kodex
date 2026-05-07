---
doc_id: API-CK8S-PROJ-0001
type: api-contract
title: kodex — API-обзор project-catalog
status: active
owner_role: SA
created_at: 2026-05-05
updated_at: 2026-05-06
related_issues: [628, 629, 630, 631, 632, 633]
related_prs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-05-wave8-project-catalog-kickoff"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-05
---

# API-обзор: project-catalog

## TL;DR

- Тип API: внутренний gRPC `ProjectCatalogService`, доменные события `project.*`.
- Аутентификация: через gateway, сервисный токен или MCP-границу; команды дополнительно проверяются через `access-manager`.
- Версионирование: стабильный транспортный `v1`; источники правды — proto и AsyncAPI.
- Основные операции: проекты, репозитории, политика `services.yaml`, источники документации, правила веток, релизная политика, релизные линии, политика размещения, политика рабочего контура.

## Спецификации

- gRPC proto: `proto/kodex/projects/v1/project_catalog.proto`.
- AsyncAPI: `specs/asyncapi/project-catalog.v1.yaml`.
- Внешний HTTP для операторской и администраторской консоли: через тонкий `staff-gateway` с OpenAPI-контрактом, не напрямую из доменного сервиса.

Этот документ фиксирует обзор операций и событий. Фактическими источниками правды для транспорта являются proto и AsyncAPI; если описание ниже расходится с машинной спецификацией, исправляется документ или контракт в том же PR.

## Операции

`ImportServicesPolicy` принимает нормализованный `validated_payload_json` как источник построения активной проекции. Транспортное поле `service_descriptors` сохранено в `v1` для совместимости контракта, но не является источником канонической проекции: если `valid` payload не содержит сервисных записей, команда должна вернуть `invalid_argument`.

Нормализованный payload также содержит источники документации. Для `valid` политики сервис проверяет scope, путь рабочего контура, режим доступа и связь с сервисами или зависимостями, затем атомарно синхронизирует источники документации, управляемые политикой, вместе с импортом политики. `project-catalog` не выполняет checkout: `GetWorkspacePolicy` возвращает только разрешённый состав источников для `agent-manager` и `runtime-manager`.

| Операция | Вид | Доступ | Идемпотентность | Примечание |
|---|---|---|---|---|
| `CreateProject` | gRPC command | `project.create` | `CommandMeta.command_id` | Создаёт проект, включая опциональную ссылку на иконку. |
| `UpdateProject` | gRPC command | `project.update` | ожидаемая версия | Обновляет название, описание, статус и ссылку на иконку. |
| `GetProject` | gRPC query | `project.read` | нет | Авторитетное чтение проекта. |
| `ListProjects` | gRPC query | `project.list` | нет | Пакетное чтение для внутренних сервисов и `staff-gateway`. |
| `AttachRepository` | gRPC command | `repository.attach` | `CommandMeta.command_id` | Привязывает репозиторий к проекту. |
| `UpdateRepository` | gRPC command | `repository.update` | ожидаемая версия | Обновляет статус, ссылку на иконку и поля политики привязки. |
| `DetachRepository` | gRPC command | `repository.detach` | ожидаемая версия | Архивирует привязку репозитория и убирает её из активной политики проекта. |
| `GetRepository` | gRPC query | `repository.read` | нет | Авторитетное чтение привязки репозитория. |
| `ListRepositories` | gRPC query | `repository.list` | нет | Список репозиториев проекта. |
| `ImportServicesPolicy` | gRPC command | `project.policy.import` | `CommandMeta.command_id` | Импортирует `services.yaml`, управляемый через Git, после первичной загрузки, слияния PR или сверки и сохраняет проверенную проекцию. |
| `GetServicesPolicy` | gRPC query | `project.policy.read` | нет | Читает активную проверенную проекцию `services.yaml`. |
| `ListServiceDescriptors` | gRPC query | `project.policy.read` | нет | Читает типизированный список сервисов из последней политики `valid + synced/overridden`. |
| `CreatePolicyEditProposal` | gRPC command | `project.policy.propose` | `CommandMeta.command_id` | Создаёт запрос на PR-изменение `services.yaml` вместо прямой записи в БД. |
| `CreatePolicyOverride` | gRPC command | `project.policy.override` | `CommandMeta.command_id` | Создаёт временное операторское переопределение с причиной, сроком действия и аудитом. |
| `CancelPolicyOverride` | gRPC command | `project.policy.override.cancel` | ожидаемая версия | Досрочно отменяет активное операторское переопределение. Причина берётся из command meta и аудита запроса. |
| `ListPolicyOverrides` | gRPC query | `project.policy.override.read` | нет | Читает активные или исторические операторские переопределения политики. |
| `PutDocumentationSource` | gRPC command | `project.docs.update` | ожидаемая версия | Обновляет источник документации. |
| `GetDocumentationSource` | gRPC query | `project.docs.read` | нет | Читает конкретный источник документации. |
| `ListDocumentationSources` | gRPC query | `project.docs.read` | нет | Читает источники документации проекта, репозитория или сервиса. |
| `GetWorkspacePolicy` | gRPC query | `project.workspace.read` | нет | Возвращает разрешённый состав рабочего контура из активной проверенной политики и активные операторские переопределения. |
| `PutBranchRules` | gRPC command | `project.branch_rules.update` | ожидаемая версия | Обновляет правила веток. |
| `GetBranchRules` | gRPC query | `project.branch_rules.read` | нет | Читает конкретный набор правил веток. |
| `ListBranchRules` | gRPC query | `project.branch_rules.read` | нет | Читает активные правила веток проекта или репозитория. |
| `PutReleasePolicy` | gRPC command | `project.release_policy.update` | ожидаемая версия | Обновляет релизную политику. |
| `GetReleasePolicy` | gRPC query | `project.release_policy.read` | нет | Читает конкретную релизную политику. |
| `ListReleasePolicies` | gRPC query | `project.release_policy.read` | нет | Читает релизные политики проекта. |
| `PutReleaseLine` | gRPC command | `project.release_line.update` | ожидаемая версия | Обновляет конкретную релизную линию. |
| `GetReleaseLine` | gRPC query | `project.release_line.read` | нет | Читает конкретную релизную линию. |
| `ListReleaseLines` | gRPC query | `project.release_line.read` | нет | Читает релизные линии проекта или релизной политики. |
| `PutPlacementPolicy` | gRPC command | `project.placement_policy.update` | ожидаемая версия | Обновляет допустимые контуры размещения. |
| `GetPlacementPolicy` | gRPC query | `project.placement_policy.read` | нет | Читает конкретную политику размещения. |
| `ListPlacementPolicies` | gRPC query | `project.placement_policy.read` | нет | Читает политики размещения проекта, репозитория или сервиса. |

## Модель ошибок

| Ошибка | Когда возвращается |
|---|---|
| `invalid_argument` | Невалидный slug, идентичность провайдера, шаблон ветки, путь рабочего контура или содержимое политики. |
| `permission_denied` | `access-manager` запретил действие. |
| `not_found` | Проект, репозиторий или политика не найдены. |
| `already_exists` | Дубликат slug проекта или идентичности провайдера у активного репозитория. |
| `failed_precondition` | Нельзя применить политику к архивному проекту или отключённому репозиторию. |
| `aborted` | Конфликт ожидаемой версии. |
| `unavailable` | Временная ошибка зависимости или БД. |

## События

События фиксируют бизнес-факты жизненного цикла, а не полный CRUD. Физическое удаление не входит в штатный `v1`: вместо `deleted` используются архивирование, отключение, отвязка, истечение срока или отмена.

| Event | Aggregate | Payload минимум |
|---|---|---|
| `project.project.created` | project | `project_id`, `organization_id`, `slug`, `version`; `icon_object_uri`, если задано |
| `project.project.updated` | project | `project_id`, `status`, `version`; `icon_object_uri`, если задано |
| `project.project.archived` | project | `project_id`, `status`, `version` |
| `project.project.disabled` | project | `project_id`, `status`, `version` |
| `project.repository.attached` | repository | `project_id`, `repository_id`, `provider`, `provider_owner`, `provider_name`, `version`; `icon_object_uri`, если задано |
| `project.repository.updated` | repository | `project_id`, `repository_id`, `status`, `version`; `icon_object_uri`, если задано |
| `project.repository.detached` | repository | `project_id`, `repository_id`, `status`, `version` |
| `project.services_policy.imported` | services_policy | `project_id`, `policy_id`, `policy_version`, `source_commit_sha`, `content_hash`; `source_blob_sha` передаётся, когда доступен у провайдера |
| `project.policy_override.created` | policy_override | `project_id`, `override_id`, `target_type`, `expires_at` |
| `project.policy_override.expired` | policy_override | `project_id`, `override_id`, `target_type` |
| `project.policy_override.cancelled` | policy_override | `project_id`, `override_id`, `target_type` |
| `project.documentation_source.created` | documentation_source | `project_id`, `source_id`, `scope_type`, `access_mode` |
| `project.documentation_source.updated` | documentation_source | `project_id`, `source_id`, `scope_type`, `access_mode` |
| `project.documentation_source.disabled` | documentation_source | `project_id`, `source_id`, `status` |
| `project.branch_rules.created` | branch_rules | `project_id`, `branch_rules_id`, `version` |
| `project.branch_rules.updated` | branch_rules | `project_id`, `branch_rules_id`, `version` |
| `project.branch_rules.disabled` | branch_rules | `project_id`, `branch_rules_id`, `status`, `version` |
| `project.release_policy.created` | release_policy | `project_id`, `release_policy_id`, `version` |
| `project.release_policy.updated` | release_policy | `project_id`, `release_policy_id`, `version` |
| `project.release_policy.archived` | release_policy | `project_id`, `release_policy_id`, `status`, `version` |
| `project.release_policy.disabled` | release_policy | `project_id`, `release_policy_id`, `status`, `version` |
| `project.release_line.created` | release_line | `project_id`, `release_policy_id`, `release_line_id`, `version` |
| `project.release_line.updated` | release_line | `project_id`, `release_policy_id`, `release_line_id`, `version` |
| `project.release_line.archived` | release_line | `project_id`, `release_policy_id`, `release_line_id`, `status`, `version` |
| `project.release_line.disabled` | release_line | `project_id`, `release_policy_id`, `release_line_id`, `status`, `version` |
| `project.placement_policy.created` | placement_policy | `project_id`, `placement_policy_id`, `version` |
| `project.placement_policy.updated` | placement_policy | `project_id`, `placement_policy_id`, `version` |
| `project.placement_policy.disabled` | placement_policy | `project_id`, `placement_policy_id`, `status`, `version` |

## Состояние реализации

| Область | Статус |
|---|---|
| gRPC proto `ProjectCatalogService` | Стабильный `v1`, покрывает весь согласованный объём операций. |
| AsyncAPI `project.*` | Стабильный `v1`, покрывает события из этого документа. |
| Сервисный процесс `project-catalog` | Подключены entrypoint, конфигурация, health/readyz/metrics, gRPC-сервер, проверка доступа через `access-manager` и outbox-dispatcher. |
| Бизнес-обработчики gRPC | Подключены к доменному сервису для проектов, репозиториев, проверенной проекции `services.yaml`, операторских переопределений, источников документации, правил веток, релизных политик, релизных линий и политики размещения. |
| PostgreSQL и outbox | Модель БД, миграции, слой репозитория, сервисный outbox и публикация событий в `platform-event-log` подключены. |

## Совместимость

- Стабильный `v1` контракт не удаляет поля без цикла `deprecate -> migrate -> remove`.
- Если этот обзор опережает реализацию, документ поставки содержит таблицу реализованных операций и бэклог.
- gRPC-контракт не импортирует transport DTO в домен; преобразование живёт в transport caster слое.

## Апрув

- request_id: `owner-2026-05-05-wave8-project-catalog-kickoff`
- Решение: approved
- Комментарий: API-обзор `project-catalog` согласован как целевое состояние стартового среза; стабильные transport-спецификации создаются отдельным срезом.
