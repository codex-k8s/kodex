---
doc_id: API-CK8S-PROJ-0001
type: api-contract
title: kodex — API-контракт project-catalog
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

# API-контракт: project-catalog

## TL;DR

- Тип API: внутренний gRPC `ProjectCatalogService`, доменные события `project.*`.
- Аутентификация: через gateway, сервисный токен или MCP-границу; команды дополнительно проверяются через `access-manager`.
- Версионирование: стабильный `v1` контракт.
- Основные операции: проекты, репозитории, политика `services.yaml`, источники документации, правила веток, релизная политика, политика размещения, политика рабочего контура.

## Спецификации

- gRPC proto: `proto/kodex/projects/v1/project_catalog.proto`.
- AsyncAPI: `specs/asyncapi/project-catalog.v1.yaml`.
- Внешний HTTP: через будущий gateway, не напрямую из доменного сервиса.

## Операции

| Operation | Contract | Auth | Idempotency | Notes |
|---|---|---|---|---|
| `CreateProject` | gRPC command | `project.create` | `CommandMeta.command_id` | Создаёт проект. |
| `UpdateProject` | gRPC command | `project.update` | ожидаемая версия | Обновляет название, описание, статус. |
| `GetProject` | gRPC query | `project.read` | нет | Авторитетное чтение проекта. |
| `ListProjects` | gRPC query | `project.list` | нет | Пакетное чтение для UI и сервисов. |
| `AttachRepository` | gRPC command | `repository.attach` | `CommandMeta.command_id` | Привязывает репозиторий к проекту. |
| `UpdateRepository` | gRPC command | `repository.update` | ожидаемая версия | Обновляет статус и поля политики привязки. |
| `ListRepositories` | gRPC query | `repository.list` | нет | Список репозиториев проекта. |
| `PutServicesPolicy` | gRPC command | `project.policy.update` | ожидаемая версия | Сохраняет проверенную версию политики `services.yaml`. |
| `PutDocumentationSource` | gRPC command | `project.docs.update` | ожидаемая версия | Обновляет источник документации. |
| `GetWorkspacePolicy` | gRPC query | `project.workspace.read` | нет | Возвращает разрешённый состав рабочего контура. |
| `PutBranchRules` | gRPC command | `project.branch_rules.update` | ожидаемая версия | Обновляет правила веток. |
| `PutReleasePolicy` | gRPC command | `project.release_policy.update` | ожидаемая версия | Обновляет релизную политику. |
| `PutPlacementPolicy` | gRPC command | `project.placement_policy.update` | ожидаемая версия | Обновляет допустимые контуры размещения. |

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
| `project.project.created` | project | `project_id`, `organization_id`, `slug`, `version` |
| `project.project.updated` | project | `project_id`, `status`, `version` |
| `project.repository.attached` | repository | `project_id`, `repository_id`, `provider`, `provider_owner`, `provider_name`, `version` |
| `project.repository.updated` | repository | `repository_id`, `status`, `version` |
| `project.services_policy.updated` | services_policy | `project_id`, `policy_id`, `policy_version`, `content_hash` |
| `project.documentation_source.updated` | documentation_source | `project_id`, `source_id`, `scope_type`, `access_mode` |
| `project.branch_rules.updated` | branch_rules | `project_id`, `repository_id`, `version` |
| `project.release_policy.updated` | release_policy | `project_id`, `policy_id`, `version` |
| `project.placement_policy.updated` | placement_policy | `project_id`, `policy_id`, `version` |

## Совместимость

- `v1` контракт не удаляет поля без цикла `deprecate -> migrate -> remove`.
- Если контракт опережает реализацию, документ поставки содержит таблицу реализованных операций и бэклог.
- gRPC-контракт не импортирует transport DTO в домен; преобразование живёт в transport caster слое.

## Апрув

- request_id: `owner-2026-05-05-wave8-project-catalog-kickoff`
- Решение: approved
- Комментарий: API-контракт `project-catalog` согласован как целевое состояние стартового среза.
