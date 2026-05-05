---
doc_id: API-CK8S-PROJ-0001
type: api-contract
title: kodex — API-обзор project-catalog
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

# API-обзор: project-catalog

## TL;DR

- Тип API: внутренний gRPC `ProjectCatalogService`, доменные события `project.*`.
- Аутентификация: через gateway, сервисный токен или MCP-границу; команды дополнительно проверяются через `access-manager`.
- Версионирование: стабильный транспортный `v1`; источники правды — proto и AsyncAPI.
- Основные операции: проекты, репозитории, политика `services.yaml`, источники документации, правила веток, релизная политика, политика размещения, политика рабочего контура.

## Спецификации

- gRPC proto: `proto/kodex/projects/v1/project_catalog.proto`.
- AsyncAPI: `specs/asyncapi/project-catalog.v1.yaml`.
- Внешний HTTP для операторской и администраторской консоли: через тонкий `staff-gateway` с OpenAPI-контрактом, не напрямую из доменного сервиса.

Этот документ фиксирует обзор операций и событий. Фактическими источниками правды для транспорта являются proto и AsyncAPI; если описание ниже расходится с машинной спецификацией, исправляется документ или контракт в том же PR.

## Операции

| Операция | Вид | Доступ | Идемпотентность | Примечание |
|---|---|---|---|---|
| `CreateProject` | gRPC command | `project.create` | `CommandMeta.command_id` | Создаёт проект, включая опциональную ссылку на иконку. |
| `UpdateProject` | gRPC command | `project.update` | ожидаемая версия | Обновляет название, описание, статус и ссылку на иконку. |
| `GetProject` | gRPC query | `project.read` | нет | Авторитетное чтение проекта. |
| `ListProjects` | gRPC query | `project.list` | нет | Пакетное чтение для внутренних сервисов и `staff-gateway`. |
| `AttachRepository` | gRPC command | `repository.attach` | `CommandMeta.command_id` | Привязывает репозиторий к проекту. |
| `UpdateRepository` | gRPC command | `repository.update` | ожидаемая версия | Обновляет статус, ссылку на иконку и поля политики привязки. |
| `GetRepository` | gRPC query | `repository.read` | нет | Авторитетное чтение привязки репозитория. |
| `ListRepositories` | gRPC query | `repository.list` | нет | Список репозиториев проекта. |
| `ImportServicesPolicy` | gRPC command | `project.policy.import` | `CommandMeta.command_id` | Импортирует `services.yaml`, управляемый через Git, после первичной загрузки, слияния PR или сверки и сохраняет проверенную проекцию. |
| `GetServicesPolicy` | gRPC query | `project.policy.read` | нет | Читает активную проверенную проекцию `services.yaml`. |
| `ListServiceDescriptors` | gRPC query | `project.policy.read` | нет | Читает типизированный список сервисов из активной политики. |
| `CreatePolicyEditProposal` | gRPC command | `project.policy.propose` | `CommandMeta.command_id` | Создаёт запрос на PR-изменение `services.yaml` вместо прямой записи в БД. |
| `CreatePolicyOverride` | gRPC command | `project.policy.override` | `CommandMeta.command_id` | Создаёт временное операторское переопределение с причиной, сроком действия и аудитом. |
| `PutDocumentationSource` | gRPC command | `project.docs.update` | ожидаемая версия | Обновляет источник документации. |
| `GetDocumentationSource` | gRPC query | `project.docs.read` | нет | Читает конкретный источник документации. |
| `ListDocumentationSources` | gRPC query | `project.docs.read` | нет | Читает источники документации проекта, репозитория или сервиса. |
| `GetWorkspacePolicy` | gRPC query | `project.workspace.read` | нет | Возвращает разрешённый состав рабочего контура. |
| `PutBranchRules` | gRPC command | `project.branch_rules.update` | ожидаемая версия | Обновляет правила веток. |
| `GetBranchRules` | gRPC query | `project.branch_rules.read` | нет | Читает конкретный набор правил веток. |
| `ListBranchRules` | gRPC query | `project.branch_rules.read` | нет | Читает активные правила веток проекта или репозитория. |
| `PutReleasePolicy` | gRPC command | `project.release_policy.update` | ожидаемая версия | Обновляет релизную политику. |
| `GetReleasePolicy` | gRPC query | `project.release_policy.read` | нет | Читает конкретную релизную политику. |
| `ListReleasePolicies` | gRPC query | `project.release_policy.read` | нет | Читает релизные политики проекта. |
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

| Event | Aggregate | Payload минимум |
|---|---|---|
| `project.project.created` | project | `project_id`, `organization_id`, `slug`, `icon_object_uri`, `version` |
| `project.project.updated` | project | `project_id`, `status`, `icon_object_uri`, `version` |
| `project.repository.attached` | repository | `project_id`, `repository_id`, `provider`, `provider_owner`, `provider_name`, `icon_object_uri`, `version` |
| `project.repository.updated` | repository | `repository_id`, `status`, `icon_object_uri`, `version` |
| `project.services_policy.updated` | services_policy | `project_id`, `policy_id`, `policy_version`, `source_commit_sha`, `source_blob_sha`, `content_hash` |
| `project.policy_override.created` | policy_override | `project_id`, `override_id`, `target_type`, `expires_at` |
| `project.policy_override.expired` | policy_override | `project_id`, `override_id`, `target_type` |
| `project.documentation_source.updated` | documentation_source | `project_id`, `source_id`, `scope_type`, `access_mode` |
| `project.branch_rules.updated` | branch_rules | `project_id`, `repository_id`, `version` |
| `project.release_policy.updated` | release_policy | `project_id`, `policy_id`, `version` |
| `project.placement_policy.updated` | placement_policy | `project_id`, `policy_id`, `version` |

## Состояние реализации

| Область | Статус |
|---|---|
| gRPC proto `ProjectCatalogService` | Стабильный `v1`, покрывает весь согласованный объём операций. |
| AsyncAPI `project.*` | Стабильный `v1`, покрывает события из этого документа. |
| Сервисный процесс `project-catalog` | Каркас готов: entrypoint, конфигурация, health/readyz/metrics и зарегистрированный gRPC-сервер. |
| Бизнес-обработчики gRPC | Отложены до среза gRPC-операций; каркас возвращает `Unimplemented` через generated-сервер. |
| PostgreSQL и outbox | Отложены до среза модели БД и репозитория. |

## Совместимость

- Стабильный `v1` контракт не удаляет поля без цикла `deprecate -> migrate -> remove`.
- Если этот обзор опережает реализацию, документ поставки содержит таблицу реализованных операций и бэклог.
- gRPC-контракт не импортирует transport DTO в домен; преобразование живёт в transport caster слое.

## Апрув

- request_id: `owner-2026-05-05-wave8-project-catalog-kickoff`
- Решение: approved
- Комментарий: API-обзор `project-catalog` согласован как целевое состояние стартового среза; стабильные transport-спецификации создаются отдельным срезом.
