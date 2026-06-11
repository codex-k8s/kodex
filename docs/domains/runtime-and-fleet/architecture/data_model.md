---
doc_id: DM-CK8S-RUNTIME-0001
type: data-model
title: kodex — модель данных runtime-manager
status: active
owner_role: SA
created_at: 2026-05-07
updated_at: 2026-06-11
related_issues: [655, 657, 658, 659, 660, 662, 966, 994]
related_prs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-07-runtime-manager-kickoff"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-07
---

# Модель данных: runtime-manager

## TL;DR

- Ключевые сущности: `Slot`, `WorkspaceMaterialization`, `Job`, `JobStep`, `RuntimeArtifactRef`, `CleanupPolicy`, `PrewarmPool` и локальный outbox.
- Все ссылки на проекты, репозитории, agent run, provider artifacts, пакеты и fleet scope хранятся как внешние идентификаторы без SQL-связей с чужими БД.
- Полный лог, Kubernetes events, registry catalog и образы не хранятся в БД runtime.

## Базовые правила

- БД `runtime-manager` принадлежит только `runtime-manager`.
- Таблицы не имеют `FOREIGN KEY` в БД других сервисов.
- Состояние меняется короткими транзакциями с версией агрегата.
- Долгие операции выполняются через job/worker/Kubernetes и фиксируют прогресс отдельными командами.
- События записываются в локальный outbox и доставляются в `platform-event-log`.
- Сырые секреты в БД не хранятся.

## Сущности

### `Slot`

Назначение: изолированная среда исполнения под работу агента, техническую проверку или проектное окружение.

Важные инварианты:

- первая физическая форма — Kubernetes namespace;
- доменная модель не зашивает namespace как единственную будущую форму;
- слот может ссылаться на agent run, но не владеет им;
- lease защищает слот от одновременного использования;
- активная попытка подготовки workspace хранится на слоте, чтобы поздний исполнитель старой попытки не перезаписал результат новой попытки или release/cleanup.

| Поле | Тип | Nullable | Ограничения | Примечание |
|---|---|---:|---|---|
| `id` | UUID | no | primary key | Идентификатор слота. |
| `slot_key` | text | no | unique | Читаемый ключ для диагностики. |
| `status` | text | no | indexed | `prewarmed`, `reserved`, `materializing`, `ready`, `in_use`, `releasing`, `failed`, `cleanup_pending`, `cleaned`. |
| `runtime_mode` | text | no | indexed | `code_only`, `full_env`, `read_only_production`. |
| `is_prewarmed` | boolean | no | default false | Создан заранее, но ещё не привязан к задаче. |
| `fleet_scope_id` | UUID | yes | indexed | Внешняя ссылка на scope размещения. |
| `cluster_id` | UUID | yes | indexed | Внешняя ссылка на кластер fleet. |
| `namespace_name` | text | no | default '' | Kubernetes namespace первой версии. |
| `agent_run_id` | UUID | yes | indexed | Внешняя ссылка на `Run` из `agent-manager`. |
| `project_id` | UUID | yes | indexed | Внешняя ссылка на проект. |
| `repository_ids_json` | jsonb | no | default [] | Внешние repository ids из `project-catalog`. |
| `active_workspace_materialization_id` | UUID | yes | indexed | Текущая попытка подготовки workspace, которая имеет право менять состояние слота. |
| `runtime_profile` | text | no | indexed | Профиль runtime, например `code-only-go`, `full-env-web`. |
| `fingerprint` | text | no | default '' | Отпечаток безопасного reuse. |
| `lease_owner` | text | no | default '' | Короткая аренда слота. |
| `lease_until` | timestamptz | yes | indexed | Истечение аренды. |
| `last_error_code` | text | no | default '' | Классификация последней ошибки. |
| `last_error_message` | text | no | default '' | Короткое сообщение без секрета. |
| `created_at` | timestamptz | no | indexed | Создание; возвращается в read contract. |
| `updated_at` | timestamptz | no | indexed | Последнее изменение; возвращается в read contract. |
| `version` | bigint | no | monotonic | Оптимистичная конкуренция. |

### `WorkspaceMaterialization`

Назначение: попытка подготовки локального набора источников внутри слота.

Важные инварианты:

- состав источников приходит из workspace policy, которой владеет другой домен;
- runtime хранит результат подготовки, а не проектную политику как истину;
- writable/read-only режим источников фиксируется явно;
- `project_id` полученной workspace policy должен совпадать с `project_id` слота.

| Поле | Тип | Nullable | Ограничения | Примечание |
|---|---|---:|---|---|
| `id` | UUID | no | primary key | Идентификатор попытки. |
| `slot_id` | UUID | no | indexed | Ссылка внутри БД runtime. |
| `status` | text | no | indexed | `pending`, `running`, `completed`, `failed`, `cancelled`. |
| `policy_digest` | text | no | indexed | Digest полученной workspace policy. |
| `sources_json` | jsonb | no |  | Нормализованный список source refs, local path и access mode. |
| `fingerprint` | text | no | indexed | Отпечаток подготовленного workspace. |
| `started_at` | timestamptz | yes |  | Начало. |
| `finished_at` | timestamptz | yes |  | Завершение. |
| `last_error_code` | text | no | default '' | Классификация ошибки. |
| `last_error_message` | text | no | default '' | Короткое сообщение без секрета. |
| `created_at` | timestamptz | no | indexed | Внутреннее persistence-поле; в текущий read contract не входит. |
| `updated_at` | timestamptz | no | indexed | Внутреннее persistence-поле; в текущий read contract не входит. |
| `version` | bigint | no | monotonic | Версия попытки. |

### `BuildContext`

Назначение: runtime-owned запрос подготовки build context для self-deploy build.

Важные инварианты:

- build context не хранится в `services.yaml`, потому что ref/PVC/digest зависят от конкретного merge commit;
- запись идемпотентна по `context_fingerprint`, который строится из project/repository/provider/source refs, source commit SHA, affected service keys и `build_plan_fingerprint`;
- `pending` с `source_snapshot_unavailable` означает ожидание checked source snapshot ref/digest от provider/project/source-artifact контура, а не ошибку Kaniko;
- `ready` допустим только при наличии `build_context_ref` и `build_context_digest`;
- БД хранит только refs, digest, status, безопасную ошибку, `next_action` и timestamps; содержимое snapshot/context, raw webhook body, diff, provider response, полный YAML, kubeconfig и значения секретов не сохраняются.

| Поле | Тип | Nullable | Ограничения | Примечание |
|---|---|---:|---|---|
| `id` | UUID | no | primary key | Идентификатор запроса подготовки. |
| `status` | text | no | indexed | `pending`, `running`, `ready`, `failed`. |
| `project_id` | UUID | no | indexed | Внешняя ссылка на проект. |
| `repository_id` | UUID | no | indexed | Внешняя ссылка на repository binding. |
| `provider` | text | no |  | Ключ провайдера. |
| `provider_owner` | text | no |  | Безопасная ссылка на owner/group. |
| `provider_name` | text | no |  | Безопасная ссылка на repository name. |
| `source_ref` | text | no |  | Branch/tag/provider ref. |
| `source_commit_sha` | text | no | indexed | Commit, для которого готовится context. |
| `affected_service_keys_json` | jsonb | no | array | Отсортированный список service keys из checked build plan. |
| `build_plan_fingerprint` | text | no |  | Fingerprint проверенного build plan. |
| `context_fingerprint` | text | no | unique | Идемпотентный fingerprint запроса. |
| `source_snapshot_ref` | text | no | default '' | Checked source snapshot ref, когда он доступен. |
| `source_snapshot_digest` | text | no | default '' | Digest checked source snapshot. |
| `build_context_ref` | text | no | default '' | Runtime-owned prepared context ref, например PVC ref. |
| `build_context_digest` | text | no | default '' | Digest prepared context. |
| `started_at` | timestamptz | yes |  | Начало подготовки. |
| `finished_at` | timestamptz | yes |  | Завершение подготовки. |
| `last_error_code` | text | no | default '' | Классификация последней безопасной ошибки. |
| `last_error_message` | text | no | default '' | Короткая сводка без секрета. |
| `next_action` | text | no | default '' | Ближайшее действие автоматизации или оператора. |
| `created_at` | timestamptz | no | indexed | Создание. |
| `updated_at` | timestamptz | no | indexed | Последнее изменение. |
| `version` | bigint | no | monotonic | Оптимистичная конкуренция. |

### `Job`

Назначение: техническая операция платформы.

Важные инварианты:

- `Job` не является agent `Run`;
- job может быть связан со слотом, проектом, release line, пакетом или maintenance policy;
- идемпотентный след mutating-команд хранится отдельно в `RuntimeManagerCommandResult`;
- захват задания является короткой арендой с токеном, чтобы поздний исполнитель не мог перезаписать новую попытку;
- `deploy` с валидным `DeployExecutionSpec` хранится и читается как типизированное задание, но spec обязан ссылаться на checked manifest bundle, rollout targets и expected image refs; задание остаётся `pending` с `deploy_executor_unavailable` и не выдаётся в claim до реализации исполнителя выкладки;
- долгие операции не держат SQL-блокировки.

| Поле | Тип | Nullable | Ограничения | Примечание |
|---|---|---:|---|---|
| `id` | UUID | no | primary key | Идентификатор задания. |
| `command_id` | text | no | unique | Идемпотентность команды создания. |
| `job_type` | text | no | indexed | `mirror`, `build`, `deploy`, `cleanup`, `health_check`, `housekeeping`, `workspace_materialization`, `agent_run`. |
| `status` | text | no | indexed | `pending`, `claimed`, `running`, `succeeded`, `failed`, `cancelled`, `timed_out`. |
| `priority` | text | no | indexed | `low`, `normal`, `high`, `blocking`. |
| `job_input_json` | jsonb | no | default {} | Ограниченный вход технической операции без секретов; для `agent_run` исполнимый вход хранится только как `agent_run_execution_spec` с refs/digest/fingerprint, обязательным workspace PVC ref, runner image/profile refs, reporting refs и вложенным `CodexSessionExecutionSpec` с проверенными instruction/result refs без raw prompt text. Для `build` исполнимый вход хранится только как `build_execution_spec`: source ref/commit SHA, `service_key`, image ref/tag/optional digest, build context ref/digest, Dockerfile ref/digest, target, builder image ref, build plan fingerprint и refs без значений секретов. Для `deploy` исполнимый вход хранится только как `deploy_execution_spec`: source ref/commit SHA, `service_key`, image ref/tag/digest, manifest/kustomization refs/digests, checked `manifest_bundle_ref`/`manifest_bundle_digest`, rollout targets, expected image refs, target namespace/cluster ref, optional slot ref, deploy plan fingerprint и refs без значений секретов. |
| `lease_owner` | text | no | default '' | Worker или controller, который забрал задание. |
| `lease_token_hash` | text | no | default '' | Хэш токена, который должен прийти в командах отчёта, завершения и ошибки. |
| `lease_until` | timestamptz | yes | indexed | Истечение аренды задания. |
| `claim_attempt` | bigint | no | default 0 | Номер попытки захвата для диагностики и защиты от поздних исполнителей. |
| `slot_id` | UUID | yes | indexed | Ссылка внутри БД runtime. |
| `agent_run_id` | UUID | yes | indexed | Внешняя ссылка на `Run`. |
| `project_id` | UUID | yes | indexed | Внешняя ссылка на проект. |
| `repository_id` | UUID | yes | indexed | Внешняя ссылка на репозиторий. |
| `release_line_id` | UUID | yes | indexed | Внешняя ссылка на release line из проектного или governance контура. |
| `package_installation_id` | UUID | yes | indexed | Внешняя ссылка на установку пакета. |
| `fleet_scope_id` | UUID | yes | indexed | Внешний fleet scope. |
| `cluster_id` | UUID | yes | indexed | Внешний cluster ref. |
| `requested_by` | UUID | yes | indexed | Actor, если применимо. |
| `created_at` | timestamptz | no | indexed | Создание; возвращается в read contract. |
| `started_at` | timestamptz | yes |  | Начало исполнения. |
| `finished_at` | timestamptz | yes |  | Завершение. |
| `next_action` | text | no | default '' | Что ожидается дальше. |
| `last_error_code` | text | no | default '' | Классификация последней ошибки. |
| `last_error_message` | text | no | default '' | Короткая ошибка без секрета. |
| `short_log_tail` | text | no | default '' | Ограниченный хвост лога. |
| `full_log_ref` | text | no | default '' | Ссылка на полный лог в Kubernetes или внешнем логировании. |
| `updated_at` | timestamptz | no | indexed | Внутреннее persistence-поле; в текущий read contract не входит. |
| `version` | bigint | no | monotonic | Оптимистичная конкуренция. |

### `RuntimeManagerCommandResult`

Назначение: persistent trail идемпотентных mutating-команд.

Инварианты:

- применяется ко всем mutating RPC, которые принимают `CommandMeta`;
- `command_id` глобально уникален для повторяемой команды;
- `idempotency_key` уникален в рамках пары actor + операция;
- `result_payload` хранит ограниченный результат без секретов, достаточный для безопасного повтора.
- Для `ClaimRunnableJob` результат команды фиксирует захваченную job, но не хранит и не возвращает `lease_token`; повтор команды завершается conflict, чтобы не захватить другую job при сетевом retry.

| Поле | Тип | Nullable | Ограничения | Примечание |
|---|---|---:|---|---|
| `key` | text | no | primary key | Стабильный ключ результата команды. |
| `command_id` | UUID | yes | unique when present | Идентификатор команды. |
| `idempotency_key` | text | no | unique with actor and operation when non-empty | Ключ идемпотентности для клиентов без UUID-команды. |
| `actor_type` | text | no | indexed | Тип субъекта, в рамках которого действует `idempotency_key`. |
| `actor_id` | text | no | indexed | Идентификатор субъекта, в рамках которого действует `idempotency_key`. |
| `operation` | text | no | indexed | Имя mutating RPC или внутренней команды. |
| `aggregate_type` | text | no | indexed | Тип агрегата результата: `slot`, `workspace_materialization`, `job`, `runtime_artifact_ref`, `cleanup_policy`, `prewarm_pool`. |
| `aggregate_id` | UUID | no | indexed | Идентификатор агрегата результата. |
| `result_payload` | jsonb | no | default {} | Ограниченный payload результата без секретов. |
| `created_at` | timestamptz | no | indexed | Время фиксации результата. |

### `JobStep`

Назначение: этап выполнения platform job.

| Поле | Тип | Nullable | Ограничения | Примечание |
|---|---|---:|---|---|
| `id` | UUID | no | primary key | Идентификатор шага. |
| `job_id` | UUID | no | indexed | Ссылка внутри БД runtime. |
| `step_key` | text | no | indexed | `checkout`, `build`, `push`, `deploy`, `smoke`, `cleanup`. |
| `status` | text | no | indexed | `pending`, `running`, `succeeded`, `failed`, `skipped`. |
| `started_at` | timestamptz | yes |  | Начало. |
| `finished_at` | timestamptz | yes |  | Завершение. |
| `short_log_tail` | text | no | default '' | Ограниченный хвост шага. |
| `external_ref` | text | no | default '' | Kubernetes Job/Pod или внешний ref. |
| `error_code` | text | no | default '' | Классификация ошибки. |
| `error_message` | text | no | default '' | Короткое сообщение. |
| `created_at` | timestamptz | no | indexed | Внутреннее persistence-поле; в текущий read contract не входит. |
| `updated_at` | timestamptz | no | indexed | Внутреннее persistence-поле; в текущий read contract не входит. |
| `version` | bigint | no | monotonic | Версия шага. |

### `RuntimeArtifactRef`

Назначение: ссылка на внешний технический артефакт среды.

Важные инварианты:

- не является реестром образов;
- не хранит blob, manifest или полный registry catalog;
- нужна для диагностики, продолжения job и связи с release evidence.

| Поле | Тип | Nullable | Ограничения | Примечание |
|---|---|---:|---|---|
| `id` | UUID | no | primary key | Идентификатор ссылки. |
| `job_id` | UUID | yes | indexed | Связанный job. |
| `slot_id` | UUID | yes | indexed | Связанный slot. |
| `artifact_type` | text | no | indexed | `image_ref`, `kubernetes_job`, `namespace`, `deployment`, `log_ref`, `manifest_ref`. |
| `external_ref` | text | no | indexed | URI/ref первоисточника. |
| `digest` | text | no | default '' | Digest, если известен. |
| `metadata_json` | jsonb | no | default {} | Ограниченная диагностика без секретов. |
| `created_at` | timestamptz | no | indexed | Создание; возвращается в read contract. |

### `CleanupPolicy`

Назначение: правило срока хранения и очистки runtime-объектов.

| Поле | Тип | Nullable | Ограничения | Примечание |
|---|---|---:|---|---|
| `id` | UUID | no | primary key | Идентификатор policy. |
| `scope_type` | text | no | indexed | `platform`, `project`, `repository`, `runtime_profile`; `organization` зарезервирован до появления projection организации на runtime-слотах и отклоняется командами cleanup. |
| `scope_id` | text | no | indexed | Внешний scope id; для `platform` пустая строка, для `runtime_profile` ключ профиля, для `project`/`repository` ненулевой UUID в канонической форме с нижним регистром. |
| `ttl_seconds` | bigint | no |  | Срок хранения после завершения. |
| `failed_ttl_seconds` | bigint | no |  | Срок хранения failed объектов. |
| `keep_short_log_tail` | boolean | no | default true | Оставлять короткий хвост. |
| `status` | text | no | indexed | `active`, `disabled`, `superseded`. |
| `created_at` | timestamptz | no |  | Создание; возвращается в read contract. |
| `updated_at` | timestamptz | no |  | Обновление; возвращается в read contract. |
| `version` | bigint | no | monotonic | Версия policy. |

### `PrewarmPool`

Назначение: управляемый пул прогретых слотов.

| Поле | Тип | Nullable | Ограничения | Примечание |
|---|---|---:|---|---|
| `id` | UUID | no | primary key | Идентификатор пула. |
| `scope_type` | text | no | indexed | `platform`, `organization`, `project`, `repository`. |
| `scope_id` | text | no | indexed | Внешний scope id; для `platform` пустая строка, для `project`/`repository` ненулевой UUID в канонической форме с нижним регистром. |
| `runtime_profile` | text | no | indexed | Профиль runtime. |
| `fleet_scope_id` | UUID | yes | indexed | Внешний fleet scope. |
| `target_size` | bigint | no |  | Желаемое число прогретых слотов. |
| `status` | text | no | indexed | `active`, `paused`, `disabled`. |
| `last_capacity_status` | text | no | default '' | `ok`, `degraded`, `insufficient`. |
| `created_at` | timestamptz | no |  | Создание; возвращается в read contract. |
| `updated_at` | timestamptz | no |  | Обновление; возвращается в read contract. |
| `version` | bigint | no | monotonic | Версия пула. |

### `RuntimeManagerOutboxEvent`

Назначение: локальная очередь доменных событий, которые `runtime-manager` должен доставить в общий `platform-event-log`.

Поля должны соответствовать общему `libs/go/outbox` adapter pattern и использовать отдельный publisher в `platform-event-log`.

## Индексы

Минимально нужны индексы:

- `Slot(status, lease_until)`;
- `Slot(project_id, status)`;
- `Slot(agent_run_id)`;
- `WorkspaceMaterialization(slot_id, status)`;
- `WorkspaceMaterialization(fingerprint)`;
- `RuntimeManagerCommandResult(command_id)`;
- `RuntimeManagerCommandResult(operation, actor_type, actor_id, idempotency_key)`;
- `RuntimeManagerCommandResult(aggregate_type, aggregate_id, created_at)`;
- `Job(status, lease_until, priority, created_at)`;
- `Job(slot_id, status)`;
- `Job(project_id, status)`;
- `Job(agent_run_id)`;
- `JobStep(job_id, status)`;
- `RuntimeArtifactRef(job_id)`;
- `CleanupPolicy(scope_type, scope_id, status)`;
- `PrewarmPool(scope_type, scope_id, runtime_profile, status)`.

## Что не хранится

| Внешний владелец | Что остаётся у него |
|---|---|
| Kubernetes | Полное состояние pod/job/deployment, events и контейнерные логи. |
| Registry | Образы, теги, manifests, layers и blobs. |
| GitHub/GitLab | `Issue`, `PR/MR`, comments, branches, tags и review. |
| `project-catalog` | Проектная политика, `services.yaml`, release policy и источники документации как истина. |
| `agent-manager` | Agent `Run`, сессии, flow, роли и acceptance. |

## Апрув

- request_id: `owner-2026-05-07-runtime-manager-kickoff`
- Решение: approved
- Комментарий: модель данных `runtime-manager` согласована как целевое состояние.
