---
doc_id: API-CK8S-RUNTIME-0001
type: api-contract
title: kodex — API-контракт runtime-manager
status: active
owner_role: SA
created_at: 2026-05-07
updated_at: 2026-05-07
related_issues: [655, 656]
related_prs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-07-runtime-manager-kickoff"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-07
---

# API-контракт: runtime-manager

## TL;DR

- Тип API: внутренний gRPC для команд и чтений, AsyncAPI для `runtime.*` событий.
- Аутентификация: внутренний сервисный контур; команды принимают `CommandMeta` и проверяют actor/service policy через `access-manager`, когда команда инициируется пользователем или сервисом с доменным риском.
- Версионирование: стабильный `v1` должен быть создан в контрактном срезе до реализации операций.
- Основные операции: слоты, workspace materialization, platform jobs, job steps, short log tail, runtime artifact refs, cleanup policy и prewarm pools.

## Спецификации

| Контракт | Источник правды |
|---|---|
| gRPC proto | `proto/kodex/runtime/v1/runtime_manager.proto` |
| AsyncAPI | `specs/asyncapi/runtime-manager.v1.yaml` |
| Go-контракты событий | `libs/go/platformevents/runtime/events.gen.go` |

Файлы спецификаций созданы в контрактном срезе. Этот документ фиксирует смысл операций, а proto и AsyncAPI остаются машинными источниками правды для транспорта и событий.

## Группы операций

### Подготовка runtime

| Операция | Назначение | Вызывает | Идемпотентность |
|---|---|---|---|
| `PrepareRuntime` | Фасадная команда для типового агентного запуска: получить fleet scope через `fleet-manager`, выделить слот, запустить подготовку workspace и вернуть контекст runtime. | `agent-manager` | `command_id`; повтор возвращает тот же результат выделения слота и подготовки workspace или актуальный конфликт. |

`PrepareRuntime` не создаёт agent `Run`, не меняет flow и не выбирает инфраструктуру самостоятельно. Он принимает внешний `agent_run_id`, runtime profile, workspace policy и placement constraints. Если явный fleet scope не передан вызывающей стороной, `runtime-manager` обращается к `fleet-manager.ResolvePlacement` и исполняет полученное решение размещения. Внутри домена команда использует те же инварианты, что `ReserveSlot` и `StartWorkspaceMaterialization`, а события публикуются как `runtime.slot.*` и `runtime.workspace.*`.

### Слоты

| Операция | Назначение | Вызывает | Идемпотентность |
|---|---|---|---|
| `ReserveSlot` | Выделить или создать слот под runtime profile и workspace policy digest. | `agent-manager`, `worker` | `command_id`. |
| `ExtendSlotLease` | Продлить аренду активного слота. | `agent-manager`, `worker`, `agent-runner` через MCP | `command_id + expected_version`. |
| `ReleaseSlot` | Освободить слот после завершения работы. | `agent-manager`, `worker` | `command_id + expected_version`. |
| `MarkSlotFailed` | Перевести слот в failed с классификацией. | `worker`, runtime controller | `command_id + expected_version`. |
| `GetSlot` | Прочитать слот. | `agent-manager`, `operations-hub`, MCP | Read-only. |
| `ListSlots` | Список по проекту, статусу, runtime profile, fleet scope. | Операторский контур | Read-only. |

### Workspace materialization

| Операция | Назначение | Вызывает | Идемпотентность |
|---|---|---|---|
| `StartWorkspaceMaterialization` | Запустить подготовку источников внутри слота. | `agent-manager`, `worker` | `command_id`. |
| `ReportWorkspaceMaterializationProgress` | Обновить статус подготовки, fingerprint и ошибки. | `worker`, runtime controller | `command_id + expected_version`. |
| `GetWorkspaceMaterialization` | Прочитать попытку подготовки. | `agent-manager`, `operations-hub` | Read-only. |
| `ListWorkspaceMaterializations` | Получить историю подготовки по слоту или agent run. | Операторский контур | Read-only. |

### Platform jobs

| Операция | Назначение | Вызывает | Идемпотентность |
|---|---|---|---|
| `CreateJob` | Создать техническое задание: mirror, build, deploy, cleanup, health-check или housekeeping. | `agent-manager`, `package-hub`, release/governance контур, операторский контур | `command_id`. |
| `ClaimRunnableJob` | Забрать задание короткой арендой для исполнения и получить `lease_token`. | `worker` | `command_id` фиксируется в `RuntimeManagerCommandResult`; повтор с тем же ключом возвращает conflict без повторного захвата, потому что `lease_token` одноразовый и не хранится в открытом виде. |
| `ReportJobStepProgress` | Обновить шаг, короткий хвост лога и refs. | `worker` | `lease_token + command_id + expected_version`. |
| `CompleteJob` | Завершить задание успешно. | `worker` | `lease_token + command_id + expected_version`. |
| `FailJob` | Завершить задание ошибкой. | `worker` | `lease_token + command_id + expected_version`. |
| `CancelJob` | Отменить pending/running job по policy. | `agent-manager`, операторский контур | `command_id + expected_version`. |
| `GetJob` | Прочитать job. | `agent-manager`, `operations-hub`, MCP | Read-only. |
| `ListJobs` | Список по статусу, типу, проекту, слоту, agent run, release line. | Операторский контур | Read-only. |

### Runtime artifact refs

| Операция | Назначение | Вызывает | Идемпотентность |
|---|---|---|---|
| `RecordRuntimeArtifactRef` | Записать ссылку на image ref, Kubernetes object, log ref или manifest ref. | `worker`, runtime controller | `command_id`. |
| `ListRuntimeArtifactRefs` | Прочитать refs по job или slot. | `agent-manager`, `operations-hub` | Read-only. |

### Cleanup и prewarm

| Операция | Назначение | Вызывает | Идемпотентность |
|---|---|---|---|
| `CreateOrUpdateCleanupPolicy` | Настроить retention для runtime-объектов. | Операторский контур | `command_id + expected_version`. |
| `RunCleanupBatch` | Исполнить пачку очистки. | `worker` | Lease по выбранным объектам. |
| `CreateOrUpdatePrewarmPool` | Настроить пул прогретых слотов. | Операторский контур, policy automation | `command_id + expected_version`. |
| `ReconcilePrewarmPool` | Догнать фактический пул до целевого размера. | `worker` | Lease. |

## Модель ошибок

| Код | Смысл |
|---|---|
| `RUNTIME_SLOT_NOT_FOUND` | Слот не найден или недоступен. |
| `RUNTIME_SLOT_CONFLICT` | Версия или lease устарели. |
| `RUNTIME_SLOT_UNSAFE_REUSE` | Reuse запрещён из-за несовпадения fingerprint или статуса. |
| `RUNTIME_WORKSPACE_SOURCE_UNAVAILABLE` | Источник workspace недоступен. |
| `RUNTIME_WORKSPACE_POLICY_INVALID` | Переданная workspace policy не может быть исполнена runtime. |
| `RUNTIME_JOB_NOT_FOUND` | Job не найден. |
| `RUNTIME_JOB_CONFLICT` | Статус job изменился конкурентно. |
| `RUNTIME_JOB_FAILED` | Техническая операция завершилась ошибкой. |
| `RUNTIME_FLEET_SCOPE_UNAVAILABLE` | Полученный fleet scope или cluster ref недоступен. |
| `RUNTIME_KUBERNETES_ERROR` | Ошибка Kubernetes API. |
| `RUNTIME_PERMISSION_DENIED` | Действие запрещено policy или `access-manager`. |

## События

| Событие | Когда публикуется |
|---|---|
| `runtime.slot.reserved` | Слот выделен или создан. |
| `runtime.slot.lease_extended` | Аренда слота продлена. |
| `runtime.slot.released` | Слот освобождён. |
| `runtime.slot.failed` | Слот переведён в ошибку. |
| `runtime.slot.cleanup_requested` | Слот поставлен в очередь очистки. |
| `runtime.slot.cleaned` | Слот успешно очищен. |
| `runtime.workspace.materialization_started` | Подготовка workspace началась. |
| `runtime.workspace.materialization_completed` | Подготовка workspace завершилась. |
| `runtime.workspace.materialization_failed` | Подготовка workspace упала. |
| `runtime.job.created` | Создано техническое задание. |
| `runtime.job.started` | Job начал исполнение. |
| `runtime.job.step_updated` | Обновлён шаг job. |
| `runtime.job.completed` | Job завершён успешно. |
| `runtime.job.failed` | Job завершён ошибкой. |
| `runtime.job.cancelled` | Job отменён. |
| `runtime.cleanup.failed` | Очистка упала. |
| `runtime.prewarm.capacity_changed` | Изменилось состояние пула прогретых слотов. |

## Совместимость

- Контракты `v1` должны покрыть согласованный объём домена, даже если реализация идёт несколькими срезами.
- Если контракт опережает код, delivery-документ должен содержать таблицу реализованных и отложенных операций.
- Namespace первой версии должен быть transport/runtime detail, а не вечным единственным типом слота.
- Fleet-ссылки должны быть в контракте сразу, даже если первый MVP использует default cluster.

## Апрув

- request_id: `owner-2026-05-07-runtime-manager-kickoff`
- Решение: approved
- Комментарий: API-карта `runtime-manager` согласована как целевое состояние.
