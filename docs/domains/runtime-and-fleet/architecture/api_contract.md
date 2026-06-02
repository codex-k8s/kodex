---
doc_id: API-CK8S-RUNTIME-0001
type: api-contract
title: kodex — API-контракт runtime-manager
status: active
owner_role: SA
created_at: 2026-05-07
updated_at: 2026-06-02
related_issues: [655, 656, 782, 949, 966, 975, 990, 994, 999, 1011]
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

`PrepareRuntime` не создаёт agent `Run`, не меняет flow и не выбирает инфраструктуру самостоятельно. Он принимает внешний `agent_run_id`, runtime profile, workspace policy и placement constraints, обращается к `fleet-manager.ResolvePlacement` и исполняет полученное решение размещения. Явный `preferred_fleet_scope_id` остаётся только входным ограничением для fleet, а не локальным выбором runtime. Внутри домена команда использует те же инварианты, что `ReserveSlot` и `StartWorkspaceMaterialization`, а события публикуются как `runtime.slot.*` и `runtime.workspace.*`.

Для руководящих пакетов `WorkspacePolicyInput.sources` использует `WorkspaceSource.kind=guidance_package`. Входной источник должен быть только для чтения, иметь `local_path` вида `.kodex/guidance/<safe_local_name>`, `source_id=guidance:<package_installation_ref>`, `digest=manifest_digest` и безопасный `metadata_json` без manifest payload, секретов, scripts, assets или содержимого документов. `metadata_json` должен содержать как минимум `package_installation_ref`, `package_version_ref`, `package_ref`, `package_slug` и `safe_local_name`. Если в наборе источников конфликтуют локальные пути или `safe_local_name` не проходит строгую ASCII-проверку, runtime отклоняет policy до materialization.

Runtime-контур не должен выводить способ получения пакета из одной строки `source_ref`. Перед checkout он обязательно читает `PackageVersion` и `PackageSource` в `package-hub` по `package_version_ref`/`package_ref`, получает `PackageVersion.source_ref.kind`, `PackageVersion.source_ref.ref`, `PackageVersion.source_ref.commit_sha` и идентичность источника, затем сверяет `manifest_digest` с refs запуска. Расхождение считается `failed_precondition`. После такого разрешения runtime может сохранить в состоянии materialization нормализованные `source_ref`, `commit_sha`, `source_ref_kind`, `source_commit_sha` и идентичность источника для fingerprint и диагностики.

Generated execution context передаётся отдельным `WorkspaceSource.kind=generated_context` с локальным путём `.kodex/context/agent-run.json`. Runtime отвечает за запись этого файла в workspace; `agent-manager` хранит только refs и runtime context.

### Слоты

| Операция | Назначение | Вызывает | Идемпотентность |
|---|---|---|---|
| `ReserveSlot` | Выделить или создать слот под runtime profile и workspace policy digest. | `agent-manager`, `worker` | `command_id`. |
| `ExtendSlotLease` | Продлить аренду активного слота. | `agent-manager`, `worker`, `agent-runner` через MCP | `command_id + expected_version`. |
| `ReleaseSlot` | Освободить слот после завершения работы. | `agent-manager`, `worker` | `command_id + expected_version`. |
| `MarkSlotFailed` | Перевести слот в failed с классификацией. | `worker`, runtime controller | `command_id + expected_version`. |
| `GetSlot` | Прочитать слот. | `agent-manager`, `operations-hub`, MCP | Read-only. |
| `ListSlots` | Список по проекту, статусу, runtime profile, fleet scope. | Операторский контур | Read-only. |

`ReserveSlot` вызывает `fleet-manager.ResolvePlacement`, если слот создаётся или переиспользуется напрямую. Runtime передаёт constraints и сохраняет только возвращённые `fleet_scope_id` и `cluster_id`; журнал решения остаётся в `fleet-manager`.

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
| `CreateJob` | Создать техническое задание: mirror, build, deploy, cleanup, health-check, housekeeping, workspace materialization или agent Run. | `agent-manager`, `package-hub`, release/governance контур, операторский контур | `command_id`. |
| `ClaimRunnableJob` | Забрать задание короткой арендой для исполнения и получить `lease_token`. | `worker` | `command_id` фиксируется в `RuntimeManagerCommandResult`; повтор с тем же ключом возвращает conflict без повторного захвата, потому что `lease_token` одноразовый и не хранится в открытом виде. |
| `ReportJobStepProgress` | Обновить шаг, короткий хвост лога и refs. | `worker` | `lease_token + command_id + expected_version`. |
| `CompleteJob` | Завершить задание успешно. | `worker` | `lease_token + command_id + expected_version`. |
| `FailJob` | Завершить задание ошибкой. | `worker` | `lease_token + command_id + expected_version`. |
| `CancelJob` | Отменить pending/running job по policy. | `agent-manager`, операторский контур | `command_id + expected_version`. |
| `GetJob` | Прочитать job. | `agent-manager`, `operations-hub`, MCP | Read-only. |
| `ListJobs` | Список по статусу, типу, проекту, слоту, agent run, release line. | Операторский контур | Read-only. |

`CreateJob` без slot получает `fleet_scope_id` и `cluster_id` через `fleet-manager.ResolvePlacement` с runtime mode `platform_job`. `CreateJob` со slot не вызывает placement повторно и наследует fleet refs из slot.

Self-deploy orchestration plan в `agent-manager` не вызывает `CreateJob`. Он хранит только safe refs, affected service keys, `services.yaml` digest/fingerprint и expected job types `build`, `deploy`, `health_check` как подготовку к owner/governance approval. Реальные runtime jobs для build/deploy должны создаваться отдельным approval-driven срезом через typed `CreateJob`, без raw webhook body, diff, полного `services.yaml`, секретов или token values.

`JOB_TYPE_AGENT_RUN` является каноническим типом задания runtime для запуска agent Run со стороны `agent-manager`. Такое задание можно создать через `CreateJob`, отфильтровать через `ListJobs` и забрать через `ClaimRunnableJob` по отдельному типу без подмены на `build`, `deploy` или `housekeeping`. Kubernetes-исполнитель `runtime-manager` забирает этот тип только при наличии валидного typed `AgentRunExecutionSpec`; задание без spec остаётся ожидающим с безопасной диагностикой `agent_run_execution_spec_required`.

Для исполнения `agent_run` используется typed `AgentRunExecutionSpec`, который формирует `agent-manager` при opt-in dispatch после готовности slot/materialization. Spec содержит только безопасные refs, digest и fingerprint: `agent_run_id`, `slot_id`, ожидаемые `workspace_materialization_id` и fingerprint, workspace mount/PVC/workspace refs, ссылку и digest `.kodex/context/agent-run.json`, `runner_profile_ref`, `runner_image_ref`, фиксированный `runner_mode`, `allowed_secret_refs` без значений и `reporting_target_refs`. Для запуска Codex-сессии `agent-manager` передаёт вложенный `CodexSessionExecutionSpec`: `instruction_object_ref`/digest, `result_schema_ref`/digest, `session_snapshot_ref` или `workspace_snapshot_ref`, `hook_endpoint_ref`, callback refs, ограниченный timeout, фиксированный runner profile, output/result refs и allowed secret refs без значений. `runtime-manager` сохраняет spec как канонический `job_input_json.agent_run_execution_spec`, сверяет slot, завершённую workspace materialization и fingerprint, не принимает legacy raw payload вместе со spec и не принимает произвольную команду. Для Kubernetes-исполнения `workspace_pvc_ref` обязателен: первый executor принимает safe PVC ref как `pvc://<namespace>/<claim>` или `k8s://pvc/<claim>` и монтирует workspace в фиксированную точку контейнера. `agent_run` без spec или без валидного вложенного execution spec остаётся созданным заданием с безопасной диагностикой `agent_run_execution_spec_required` или `agent_execution_contract_unavailable` и не попадает в claim для исполнения.

Исполнитель Kubernetes в `runtime-manager` обрабатывает ограниченные типы `health_check` и `agent_run`. Он забирает задание через `ClaimRunnableJob`, читает выбранный кластер через `fleet-manager.GetKubernetesCluster`, получает только ссылку `secret_store_type`/`secret_store_ref` и разрешает kubeconfig в памяти через `secretresolver`. Значение kubeconfig, raw Kubernetes objects, events и полный лог не пишутся в БД. Запуск фиксируется через `ReportJobStepProgress` с `RuntimeArtifactRef` на Kubernetes Job и namespace, для `agent_run` дополнительно сохраняется image ref runner-а. Завершение идёт через `CompleteJob` или `FailJob`.

Статус самого `AgentRun` остаётся в `agent-manager`. Runtime job lifecycle показывает состояние задания runtime, а `agent-runner` для продолжения orchestration сообщает bounded `queued`/`running`/`started`/`completed`/`failed`/`cancelled`/`timed_out` через `agent-manager.ReportAgentRunState` с `run_id`, `session_id`, `runtime_slot_ref` и `runtime_job_ref`. `timed_out` фиксируется в `agent-manager` как failed run с safe `failure_code`, а `cancelled` как terminal cancelled run. `runtime-manager` не пишет `Run` напрямую и не хранит prompt, transcript, raw tool payload, provider payload или значения секретов для runner report.
Kubernetes-исполнитель передаёт runner-у только адрес `agent-manager` и ссылку `SecretKeyRef` на gRPC token; значение токена не читается `runtime-manager`, не попадает в `job_input_json` и не записывается строковым env-значением.

Для `agent_run` Kubernetes Job создаётся с детерминированным именем runtime job, фиксированным контейнером `runtime-agent-runner`, image из `runner_image_ref`, фиксированной командой `/kodex/bin/agent-runner run`, выключенным automount service account token, PVC mount workspace и ограниченными env со safe refs/digest/fingerprint. `allowed_secret_refs` передаются только как JSON-список ссылок без значений; `runtime-manager` не разрешает эти secret refs и не превращает их в Kubernetes Secret values. Переменные окружения для отчёта в `agent-manager` строятся из операторской конфигурации executor-а: адрес передаётся строкой, auth token — только через Kubernetes `SecretKeyRef`.

Рабочая нагрузка `services/jobs/agent-runner` является исполняемым образом для этой команды. Production image содержит `/kodex/bin/agent-runner` и Codex CLI по фиксированному пути `/usr/local/bin/codex`; Dockerfile проверяет наличие CLI и команды `codex exec` при сборке. Runner читает только `.kodex/context/agent-run.json` из смонтированного workspace, сверяет `context_digest`, `workspace_fingerprint`, `agent_run_id`, `slot_id`, materialization refs и фиксированный `runner_mode=codex_agent`. Связь с `agent-manager` выполняется через существующие команды `GetAgentRunRuntimeStatus`, `ReportAgentRunState` и `RecordAgentActivity`, если сервисный адрес и токен переданы как конфигурация runner-а. `runtime-manager` передаёт `CodexSessionExecutionSpec` в runner как bounded JSON env с одними refs/digest. Проверенный execution input материализуется отдельно в workspace или объектном хранилище и читается runner-ом только по `instruction_object_ref`/digest; текущий runner исполняет только workspace refs вида `workspace://.kodex/execution/...`, сверяет instruction и result schema digest, передаёт instruction в `codex exec` через stdin, result schema через `--output-schema`, workspace через `--cd` и sandbox из поддержанного fixed runner profile. Его содержимое не хранится в БД и не становится частью `agent-run.json`. Если spec отсутствует, неполон, не проходит safe-валидацию, указывает неподдержанный ref/profile или digest не совпадает, runner завершает job безопасной диагностикой `agent_execution_contract_unavailable`; произвольная shell-команда, prompt body из `agent-run.json`, transcript, raw tool input/output, provider payload, kubeconfig, secret values и полный stdout/stderr не принимаются и не логируются. Успешное завершение сохраняет только bounded summary, result digest/schema ref и безопасные refs через `ReportAgentRunState` и activity timeline.

`JobInputJSON` для `health_check` не является произвольным manifest. Поддержаны только ограниченные поля `namespace`, `service_account`, `image` и `labels`; значения `env`, annotations, команды контейнера, значения секретов, prompt, transcript, provider payload и большие тексты не принимаются. Команда контейнера остаётся фиксированной проверкой здоровья. Для `agent_run` произвольный `job_input_json` также запрещён: исполнимый payload хранится только как `agent_run_execution_spec`, а runner получает фиксированный набор env и mount. Остальные типы заданий (`build`, `deploy`, нагрузки slot-агента и т.п.) не исполняются этим Kubernetes-исполнителем.

Worker Kubernetes использует lease job как границу владения исполнением. Повторный claim после истечения lease переиспользует детерминированный Kubernetes Job по имени и проверяет, что объект действительно создан `runtime-manager` для того же runtime job. Остановка процесса или отмена контекста не переводит platform job в `failed`: claim остаётся для повторной сверки после истечения lease. Реальный таймаут Kubernetes Job, условие `JobFailed`, удалённый/отменённый Kubernetes Job или недоступный статус фиксируются классифицированной ошибкой через `FailJob`. Ошибки claim/report/complete сдерживаются повтором с увеличивающейся задержкой, чтобы worker не входил в частый цикл запросов.

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
| `RUNTIME_PLACEMENT_REJECTED` | `fleet-manager` отказал в размещении по правилам, health или отсутствию подходящего кластера. |
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
- Fleet-ссылки должны быть в контракте сразу; `platform-default` является seed/fallback внутри `fleet-manager`, а не локальным выбором `runtime-manager`.

## Апрув

- request_id: `owner-2026-05-07-runtime-manager-kickoff`
- Решение: approved
- Комментарий: API-карта `runtime-manager` согласована как целевое состояние.
