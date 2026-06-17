---
doc_id: DM-CK8S-AGENT-ORCHESTRATION-0001
type: data-model
title: kodex — модель данных домена оркестрации агентов
status: active
owner_role: SA
created_at: 2026-05-12
updated_at: 2026-06-11
related_issues: [733, 749, 759, 772, 322, 782, 795, 809, 820, 834, 842, 862, 866, 891, 905, 918, 937, 954, 968, 984, 994, 1011, 1015, 1022, 1027]
related_prs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-12-agent-manager-kickoff"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-12
---

# Модель данных: оркестрация агентов

## TL;DR

- Ключевые сущности: `Flow`, `FlowVersion`, `Stage`, `StageTransition`, `RoleProfile`, `StageRoleBinding`, `PromptTemplate`, `PromptTemplateVersion`, `AgentSession`, `AgentRun`, `AgentSessionStateSnapshot`, `AgentActivity`, `AcceptanceCheck`, `AcceptanceResult`, `FollowUpIntent`, `HumanGateRequest`, `SelfDeployPlan`, `AutomationBinding`.
- Технические агрегаты: `CommandResult`, `OutboxEvent`.
- Основные связи: flow содержит этапы; этапы привязывают роли; роль использует prompt version; сессия содержит agent `Run`; `Run` фиксирует immutable-ссылки и версии flow/stage/role/prompt/guidance, а также ссылки на provider/runtime/package.
- Риски миграций: нельзя хранить runtime filesystem, provider-native истину, пакетную истину, диалоговые сообщения, секреты и полные логи в БД `agent-manager`.
- Первый контур хранения `agent-manager` покрывает flow, версии flow, этапы, переходы, роли, шаблоны prompt, версии prompt, сессии, agent `Run`, снимки состояния, безопасную activity timeline, acceptance result, follow-up intent, идемпотентные результаты команд и service-local outbox.

## Правило внешних ссылок

`agent-manager` хранит внешние ссылки как typed refs:
- `provider_work_item_ref`;
- `provider_pull_request_ref`;
- `runtime_slot_ref`;
- `runtime_job_ref`;
- `package_installation_ref`;
- `guidance_package_version_ref`;
- `interaction_thread_ref`;
- `governance_gate_ref`.

Эти ссылки не являются SQL-связями с БД других сервисов. Источник истины остаётся у сервиса-владельца.

Для self-deploy `SelfDeployPlan` хранит только safe project/provider refs, affected service keys, `services.yaml` digest/fingerprint/version, expected runtime job type hints и governance refs. После approval `agent-manager` получает build-вход через `project-catalog.GetSelfDeployBuildPlan`: `project-catalog` связывает affected service keys с checked `ServicesPolicy` и возвращает рецепт сборки или `BuildExecutionSpec`-совместимые refs для `runtime-manager`. Если project-side build plan возвращает `build_context_required`, `agent-manager` фиксирует build dispatch как `preparing_context` и не создаёт `JOB_TYPE_BUILD` до появления runtime-owned build context refs/digest; остальные non-ready статусы становятся safe `blocked` diagnostic. `agent-manager` не читает raw `services.yaml`, не вычисляет Dockerfile/image refs по путям и не хранит значения секретов.

## Сущности

### Flow

`Flow` описывает логический процесс, например полный путь от intake до ops или короткий путь исправления.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор flow. |
| `scope_type` | enum | нет | `platform`, `organization`, `project`, `repository`. |
| `scope_ref` | text | нет | Внешний идентификатор области. |
| `slug` | text | нет | Стабильный ключ в scope. |
| `display_name` | jsonb | нет | Локализованное название. |
| `description` | jsonb | нет | Локализованное описание. |
| `icon_object_uri` | text | да | Ссылка на иконку в S3-compatible объектном хранилище; бинарные данные не хранятся в БД. |
| `status` | enum | нет | `draft`, `active`, `disabled`, `archived`. |
| `active_version_id` | uuid | да | Текущая активная версия. |
| `version` | bigint | нет | Оптимистичная конкуренция для изменений flow и выбора активной версии. |
| `created_at`, `updated_at` | timestamptz | нет | Технические временные метки. |

### FlowVersion

`FlowVersion` является immutable-снимком процесса.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор версии. |
| `flow_id` | uuid | нет | Flow-владелец. |
| `version` | bigint | нет | Монотонная версия. |
| `source_ref` | text | да | Репозиторий, пакет или фикстура, если версия пришла из внешнего источника. |
| `definition_digest` | text | нет | Digest нормализованного определения. |
| `status` | enum | нет | `draft`, `active`, `superseded`, `rejected`. |
| `activated_at` | timestamptz | да | Когда версия стала активной. |
| `created_at` | timestamptz | нет | Когда версия создана. |

### Stage

`Stage` принадлежит `FlowVersion` и описывает один этап.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор этапа. |
| `flow_version_id` | uuid | нет | Версия flow. |
| `slug` | text | нет | Ключ этапа: `intake`, `prd`, `dev`, `qa`, `release` и подобные. |
| `stage_type` | enum | нет | `work`, `review`, `gate`, `release`, `ops`, `custom`. |
| `display_name` | jsonb | нет | Локализованное название. |
| `icon_object_uri` | text | да | Ссылка на иконку этапа в S3-compatible объектном хранилище. |
| `required_artifacts` | jsonb | нет | Ожидаемые `Issue`, `PR/MR`, комментарии, документы или follow-up refs. |
| `acceptance_policy` | jsonb | нет | Проверки, watermark, gates и правила перехода. |
| `position` | int | нет | Порядок отображения и базовая последовательность. |

### StageTransition

`StageTransition` описывает допустимый переход.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор перехода. |
| `flow_version_id` | uuid | нет | Версия flow. |
| `from_stage_id` | uuid | да | Пусто для стартового перехода. |
| `to_stage_id` | uuid | нет | Следующий этап. |
| `condition` | jsonb | нет | Условие перехода: acceptance, gate, branch, manual choice. |
| `follow_up_type` | text | да | Тип provider-native `Issue`, который нужно создать для следующего этапа. |
| `position` | int | нет | Порядок перехода внутри версии flow. |

### RoleProfile

`RoleProfile` описывает роль агента.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор роли. |
| `scope_type` | enum | нет | Область роли. |
| `scope_ref` | text | нет | Внешняя ссылка области. |
| `slug` | text | нет | Ключ роли. |
| `display_name` | jsonb | нет | Локализованное название. |
| `icon_object_uri` | text | да | Ссылка на иконку роли в S3-compatible объектном хранилище. |
| `role_kind` | enum | нет | `worker`, `reviewer`, `gatekeeper`, `manager`, `qa`, `ops`, `custom`. |
| `runtime_profile` | text | нет | Режим запуска: `code_only`, `full_env`, `read_only_production` или профиль проекта. |
| `allowed_mcp_tools` | jsonb | нет | Список допустимых категорий или инструментов MCP. |
| `provider_account_policy_ref` | text | да | Ссылка на policy выбора внешнего аккаунта. |
| `status` | enum | нет | `draft`, `active`, `disabled`, `archived`. |
| `version` | bigint | нет | Оптимистичная конкуренция. |
| `created_at`, `updated_at` | timestamptz | нет | Технические временные метки. |

### PromptTemplate и PromptTemplateVersion

`PromptTemplate` группирует версии prompt для роли и назначения. `PromptTemplateVersion` фиксирует источник, immutable-ссылку при необходимости и digest принятой версии.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор шаблона или версии. |
| `prompt_template_id` | uuid | да | Заполняется у версии prompt и указывает на родительский шаблон. |
| `role_profile_id` | uuid | нет | Роль-владелец. |
| `prompt_kind` | enum | нет | `work`, `revise`, `review`, `manager`, `custom`. |
| `active_version_id` | uuid | да | Заполняется у шаблона и указывает на активную версию. |
| `version` | bigint | нет | Версия шаблона для оптимистичной конкуренции или номер версии prompt. |
| `source_ref` | text | да | Репозиторий, пакет или фикстура версии. |
| `template_object_ref` | object ref | да | Ссылка на immutable-копию prompt в объектном хранилище, если версия импортирована не только из репозитория. |
| `template_digest` | text | да | Digest текста версии. |
| `status` | enum | да | Статус версии: `draft`, `active`, `superseded`, `rejected`. |
| `created_at`, `updated_at`, `activated_at` | timestamptz | да | Временные метки шаблона и версии. |

Если prompt поставляется из репозитория или пакета, БД хранит принятую runtime-версию, источник, безопасную ссылку на immutable-копию при необходимости и digest. Текст prompt не публикуется в событиях и не должен передаваться через общие транспортные модели как свободный приватный payload. Изменение через self-improve должно проходить через provider-native PR к исходному репозиторию, а не через тихое изменение активной версии.

### StageRoleBinding

`StageRoleBinding` связывает этап и роль.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор привязки. |
| `stage_id` | uuid | нет | Этап. |
| `role_profile_id` | uuid | нет | Роль. |
| `binding_kind` | enum | нет | `executor`, `reviewer`, `gatekeeper`, `qa`, `observer`, `custom`. |
| `launch_policy` | jsonb | нет | Ручной запуск, автозапуск, параллельный запуск, retry. |
| `required_for_acceptance` | bool | нет | Обязательна ли роль для перехода. |

### AgentSession

`AgentSession` описывает продолжимый логический контекст.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор сессии. |
| `scope_type` | enum | нет | Организация, проект или репозиторий. |
| `scope_ref` | text | нет | Внешний идентификатор области. |
| `provider_work_item_ref` | text | да | Основной `Issue` или другой provider target. |
| `flow_version_id` | uuid | да | Выбранная версия flow. |
| `current_stage_id` | uuid | да | Текущий этап. |
| `latest_state_snapshot_id` | uuid | да | Последний сохранённый снимок Codex session state. |
| `status` | enum | нет | `open`, `waiting`, `completed`, `failed`, `cancelled`. |
| `created_by_actor_ref` | text | нет | Кто инициировал сессию. |
| `created_at`, `updated_at` | timestamptz | нет | Технические временные метки. |

Для непустого `provider_work_item_ref` в одном `scope` допускается только одна активная `open`/`waiting` session. Новая команда с тем же provider target должна продолжать найденную session, а не создавать дубль.

### AgentRun

`AgentRun` описывает один запуск агента внутри сессии.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор запуска. |
| `session_id` | uuid | нет | Сессия-владелец. |
| `flow_version_id` | uuid | да | Версия flow, использованная именно этим запуском; не выводится из текущего состояния сессии. |
| `stage_id` | uuid | да | Этап, если run связан с flow. |
| `role_profile_id` | uuid | нет | Роль. |
| `role_profile_version` | bigint | нет | Версия профиля роли на момент запуска. |
| `role_profile_digest` | text | нет | Digest нормализованного профиля роли на момент запуска. |
| `prompt_template_version_id` | uuid | нет | Версия prompt. |
| `prompt_template_digest` | text | нет | Digest prompt version, использованной при запуске. |
| `runtime_ref` | text/json | да | Безопасные refs runtime: slot/workspace/context, `runtime_job_ref` или fingerprint подготовки без локальных workspace paths. |
| `provider_target_ref` | text | да | Основная provider-native цель. |
| `guidance_refs` | jsonb | нет | Замороженные безопасные refs руководящих пакетов: installation ref, package/version ref, manifest digest, строковый source ref как подсказка, package slug/version label, capability ref и bounded policy summary без manifest payload. |
| `status` | enum | нет | `requested`, `starting`, `running`, `waiting`, `completed`, `failed`, `cancelled`. |
| `result_summary` | text | да | Короткая безопасная сводка, включая diagnostic summary подготовки runtime без payload текстов. |
| `failure_code` | text | да | Короткий код ошибки без секретов и PII; для permanent workspace preparation failure используется машинный код подготовки. |
| `version` | bigint | нет | Оптимистичная конкуренция. |
| `started_at`, `finished_at` | timestamptz | да | Временные метки выполнения. |
| `created_at`, `updated_at` | timestamptz | нет | Технические временные метки. |

`AgentRun.status` меняется только по доменной state machine: terminal-статусы `completed`, `failed` и `cancelled` не возвращаются в работу, `running` не откатывается в `starting`, а повтор текущего non-terminal статуса допускается только как безопасная идемпотентная фиксация без нового lifecycle event.

`guidance_refs` не является manifest cache. В этом поле нельзя хранить `payload_json`, `SKILL.md`, prompt templates, flow files, scripts, assets, package source или расширенный снимок `PackageSource`. Локальные пути workspace также не являются частью модели `AgentRun`: runtime request передаёт их как часть `WorkspaceSource`, а авторитетное состояние workspace, нормализация путей и materialization остаются в `runtime-manager`. Тип source ref, commit SHA и идентичность источника runtime получает из `package-hub` по `package_version_ref` перед materialization.

Поверхность чтения `AgentRunRuntimeStatus` не добавляет новое авторитетное хранилище runtime в БД `agent-manager`. Она строится из сохранённого `AgentRun.runtime_ref`, текущего состояния `Run` и актуального ответа `runtime-manager.GetJob` по `runtime_job_ref`. Наружу отдаются только job ref, безопасный статус, command ref, версии, timestamps, safe error code/summary и признак ожидания Human gate. `job_input_json`, steps, `short_log_tail`, `full_log_ref`, workspace paths, prompt body, provider payload, kubeconfig, секреты и большие логи не сохраняются и не возвращаются из `agent-manager`.

Операторская read surface для списков не добавляет новые таблицы и не становится кэшем соседних сервисов. `ListAgentSessions` и `ListAgentRunSummaries` строятся из `agent_manager_sessions`, `agent_manager_runs`, `agent_manager_human_gate_requests`, `agent_manager_follow_up_intents` и `agent_manager_agent_activities`: наружу выходят session/run refs, scope/provider target refs, role/stage refs, сохранённый `runtime_job_ref`, safe summary/error, флаг ожидания Human gate/follow-up, latest activity summary, timestamps и version. Живой runtime job status читается отдельной точечной операцией `GetAgentRunRuntimeStatus`, а списки не делают fan-out в `runtime-manager`, Kubernetes, provider API или соседние БД. Если в модели нет assignee или live runtime detail, список возвращает честную partial-сводку без выдуманных полей.

### AgentSessionStateSnapshot

`AgentSessionStateSnapshot` хранит метаданные снимка Codex session state. Сам JSON/JSONL-файл сессии лежит в S3-compatible объектном хранилище и обновляется после каждого значимого turn/checkpoint агентного запуска.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор снимка. |
| `session_id` | uuid | нет | Агентная сессия. |
| `run_id` | uuid | да | Запуск, который записал снимок. |
| `snapshot_kind` | enum | нет | `turn_checkpoint`, `run_completion`, `manual_checkpoint`, `recovery_checkpoint`. |
| `turn_index` | bigint | да | Монотонный номер turn/checkpoint внутри сессии, если известен. |
| `object_uri` | text | нет | Ссылка на объект с JSON/JSONL state в S3-compatible хранилище. |
| `object_digest` | text | нет | Digest объекта для проверки целостности. |
| `object_size_bytes` | bigint | да | Размер объекта для лимитов и retention. |
| `captured_at` | timestamptz | нет | Когда снимок был получен от runner/runtime. |
| `created_at` | timestamptz | нет | Когда метаданные записаны в БД. |

`agent-manager` владеет только метаданными и указателем `latest_state_snapshot_id`. Загрузку объекта, проверку размера, шифрование и выдачу содержимого выполняет платформенный storage-контур; большой session JSON не хранится в PostgreSQL.

### AgentActivity

`AgentActivity` является канонической persistent-историей безопасных действий агента внутри session/run. Это состояние принадлежит `agent-manager`, потому что именно он владеет `AgentSession` и `AgentRun`; `codex-hook-ingress` остаётся sanitizer/router/realtime ops feed и не становится долгим хранилищем tool calls.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор записи timeline. |
| `session_id` | uuid | нет | Сессия-владелец. |
| `run_id` | uuid | да | Запуск, если действие связано с конкретным `Run`. |
| `turn_id` | text | да | Safe turn ref без transcript или session dump. |
| `tool_use_id` | text | да | Safe id tool call, если запись относится к tool activity. |
| `activity_kind` | enum | нет | `lifecycle`, `tool_use`, `tool_result`, `permission`, `provider_signal`, `runtime_signal`, `checkpoint`, `other`. |
| `tool_name`, `tool_category` | text | да | Safe имя и категория инструмента; для tool-scoped записей нужен хотя бы один из этих признаков. |
| `status` | enum | нет | `planned`, `started`, `succeeded`, `failed`, `denied`, `waiting`, `cancelled`, `skipped`. |
| `started_at`, `finished_at` | timestamptz | да | Время начала обязательно; окончание может отсутствовать для pending/waiting записи. |
| `duration_ms` | bigint | да | Длительность, если известна или вычислена из временных меток. |
| `safe_summary` | text | да | Короткая безопасная сводка для UI. |
| `payload_digest` | text | да | Digest очищенного payload или значимых частей, например `sha256:<hex>`. |
| `bounded_error` | text | да | Короткая безопасная ошибка без stdout/stderr/log body. |
| `safe_refs_json` | jsonb | нет | Bounded JSON-object только с refs: artifact/risk/gate/provider/runtime refs. |
| `safe_details_json` | jsonb | нет | Bounded JSON-object с безопасными display details без raw payload. |
| `correlation_id` | text | да | Trace/correlation ref. |
| `idempotency_key` | text | нет | Command idempotency trace для replay. |
| `version` | bigint | нет | Версия записи timeline. |
| `created_at`, `updated_at` | timestamptz | нет | Технические временные метки. |

В `AgentActivity` запрещено хранить `tool_input`, `tool_response`, stdout/stderr/logs, raw provider payload, prompt, transcript, session dump, kubeconfig, секреты, токены, локальные workspace paths и файлы workspace. Если нужно показать полный output или отчёт, запись должна содержать только digest, object/artifact ref и bounded summary.

### AcceptanceCheck и AcceptanceResult

`AcceptanceCheck` описывает тип проверки в policy/flow-контексте, а `AcceptanceResult` является хранимым агрегатом результата. Базовый lifecycle создаёт один pending result на команду `RequestAcceptance`, затем `RecordAcceptanceResult` переводит его в `passed`, `failed`, `waiting` или `skipped` через ожидаемую версию. Для `human_gate` acceptance фиксирует только ожидание `waiting` с безопасной ссылкой на gate/risk/governance; итог owner decision хранится в отдельном `HumanGateRequest` агрегате `agent-manager` как normalized orchestration result. `GovernanceContextRef` хранит только typed refs на `governance-manager` факты и policy refs, но не decision body, risk payload или release evidence.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор проверки или результата. |
| `session_id` | uuid | нет | Сессия. |
| `run_id` | uuid | да | Запуск, который дал артефакт. |
| `stage_id` | uuid | да | Этап. |
| `check_kind` | enum | нет | `artifact`, `watermark`, `policy`, `role_result`, `human_gate`, `follow_up`. |
| `status` | enum | нет | `pending`, `passed`, `failed`, `waiting`, `skipped`. |
| `target_ref` | text | да | Provider/runtime/package/governance/interaction ref: trim, до 512 символов, видимый ASCII safe-ref с namespace (`kind:value`) и без raw/log/secret markers. |
| `details_json` | jsonb | нет | Bounded JSON-object с безопасными `summary`, `digest`, `artifact_refs`, `risk_ref`, `gate_ref` и другими refs. |
| `governance_risk_assessment_ref`, `governance_gate_request_ref`, `governance_gate_decision_ref`, `governance_release_decision_package_ref`, `governance_release_decision_ref`, `governance_risk_profile_ref`, `governance_gate_policy_ref`, `governance_release_policy_ref` | text | да | Typed safe refs на governance/risk/release/policy контекст. `gate_decision_ref` допустим только вместе с `gate_request_ref`, а `release_decision_ref` — только вместе с `release_decision_package_ref`. |
| `version` | bigint | нет | Оптимистичная конкуренция результата приёмки. |
| `created_at`, `updated_at` | timestamptz | нет | Технические временные метки. |

`details_json` не является отчётом QA runner и не хранит raw provider payload, workspace files, prompt text, flow files, руководящие документы, stdout/stderr/logs, секреты, токены или PII. Если приёмка ждёт Human gate или governance decision, `agent-manager` фиксирует только статус ожидания и typed `governance_*_ref`; normalized результат решения записывается в `HumanGateRequest`, а transport/governance payload остаётся у сервисов-владельцев.

### HumanGateRequest

`HumanGateRequest` — авторитетная модель ожидания и результата owner decision в `agent-manager`. Она связывает решение с session/run/stage/acceptance, provider-native target refs и typed `GovernanceContextRef`, но не владеет транспортом сообщения и не хранит governance decision body. Повтор `RequestHumanGate` с тем же command/idempotency key возвращает тот же wait только при совпадении нормализованного payload. `RecordHumanGateDecision` требует expected version, переводит ожидание в `resolved` и сохраняет normalized outcome для следующего шага flow.

Request-side интеграция с `interaction-hub` включается явным runtime switch. В этом режиме `RequestHumanGate` после replay-check создаёт `interaction-hub.RequestHumanGate` с owner-side ref `agent:human_gate/<human_gate_request_id>`, source owner `agent_manager`, decision owner `agent_manager`, safe session/run/stage/provider refs, target actor ref из `AgentSession.created_by_actor_ref`, bounded `safe_summary` и действиями `approve`/`reject`/`request_changes`/`answer`. Если команда повторяется с тем же idempotency trace, `agent-manager` возвращает уже сохранённый wait и не создаёт второй transport request. Если вызов `interaction-hub` временно недоступен, локальный wait не записывается, поэтому retry сохраняет тот же owner ref и идёт с тем же interaction command identity.

Event-driven resume идёт через уже очищенное событие `interaction.request.response_recorded`. Для Human gate `interaction-hub` указывает `owner_service=agent_manager`, `request_kind=human_gate`, `owner_request_ref` на `HumanGateRequest` и safe refs `request_id`/`response_id`. Consumer `agent-manager` принимает только action/status/version/timestamps из event log и вычисленный digest безопасного event snapshot; raw response body, callback payload, transport delivery body, prompt/transcript/logs/PII не копируются. Replay одного `response_id` возвращает уже записанный result, а несовпадающий outcome, request/response ref, fingerprint или interaction request version получает conflict. `reject` означает отказ владельца от продолжения текущего шага, а `request_changes` означает запрос доработки и должен вести flow в ветку исправления или повторной проверки.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор ожидания Human gate. |
| `session_id` | uuid | нет | Сессия, которая должна продолжиться или завершиться после решения. |
| `run_id` | uuid | да | Запуск, который ждёт решения или породил артефакт. |
| `stage_id` | uuid | да | Этап flow, к которому относится ожидание. |
| `acceptance_result_id` | uuid | да | Human-gate acceptance result в статусе `waiting`, если ожидание создано из machine acceptance. |
| `provider_work_item_ref`, `provider_pull_request_ref`, `provider_comment_ref`, `provider_review_signal_ref` | text | да | Safe refs на provider-native артефакты; provider-native истина остаётся у `provider-hub`. |
| `target_ref` | text | да | Safe ref артефакта/этапа/объекта решения, до 512 символов, namespace format `kind:value`. |
| `request_kind` | text | нет | Тип owner decision wait, например `owner_decision`, `release_gate`, `clarification`. |
| `reason_code` | text | нет | Машинный safe reason для ожидания. |
| `safe_summary` | text | да | Bounded summary для UI и события; не содержит prompt, transcript, logs, PII или внешние payload. |
| `interaction_request_ref`, `interaction_response_ref` | text | да | Refs на transport lifecycle `interaction-hub`; request/response payload не копируется. |
| `governance_gate_request_ref`, `governance_decision_ref` | text | да | Backward-readable refs на gate request/decision lifecycle `governance-manager`; decision body не копируется. |
| `governance_risk_assessment_ref`, `governance_release_decision_package_ref`, `governance_release_decision_ref`, `governance_risk_profile_ref`, `governance_gate_policy_ref`, `governance_release_policy_ref` | text | да | Дополнительный typed governance/policy контекст для UI/MCP и последующих consumers без копирования внешних данных. |
| `status` | enum | нет | `requested`, `waiting`, `resolved`, `failed`, `cancelled`; запись решения переводит ожидание в `resolved`. |
| `outcome` | enum | нет | `none`, `approve`, `reject`, `request_changes`, `answer`; для `resolved` outcome не может быть `none`. |
| `idempotency_key` | text | нет | Сохранённый command idempotency trace. |
| `version` | bigint | нет | Версия wait/result для optimistic concurrency. |
| `resolved_at` | timestamptz | да | Время фиксации normalized outcome. |
| `created_at`, `updated_at` | timestamptz | нет | Технические временные метки. |

`HumanGateRequest` не хранит raw prompt/transcript, workspace paths/files, stdout/stderr/logs, provider payload, transport callback body, governance decision body, секреты, токены, email/phone/address и другой PII. Если требуется показать полный ответ владельца, UI читает его через owner-сервис `interaction-hub` с учётом доступа и retention; `agent-manager` хранит только refs и bounded summary.

### FollowUpIntent

`FollowUpIntent` описывает намерение выполнить следующий безопасный provider-native шаг. В `agent-manager` это авторитетное состояние intent и lifecycle dispatch: создание или обновление `Issue`, создание или обновление комментария, обновление `PR/MR` и создание provider-native review signal выполняются только через typed команды `provider-hub`, а `agent-manager` сохраняет результат как safe refs/status.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор намерения. |
| `session_id` | uuid | нет | Сессия. |
| `run_id` | uuid | да | `Run`, результат которого породил follow-up. Если указан `AcceptanceResult`, связь с `Run` должна совпадать. |
| `from_stage_id` | uuid | да | Исходный этап. |
| `to_stage_id` | uuid | да | Следующий этап. |
| `acceptance_result_id` | uuid | да | Положительный результат machine acceptance, если follow-up создаётся по итогам приёмки. Pending/failed/waiting acceptance не может породить intent. |
| `provider_work_item_ref`, `provider_pull_request_ref`, `provider_comment_ref`, `provider_review_signal_ref` | text | да | Безопасные provider refs. Хотя бы один target ref обязателен; значения имеют safe-ref формат `kind:value`, ограничены по длине и не содержат raw/log/secret markers. |
| `governance_risk_assessment_ref`, `governance_gate_request_ref`, `governance_gate_decision_ref`, `governance_release_decision_package_ref`, `governance_release_decision_ref`, `governance_risk_profile_ref`, `governance_gate_policy_ref`, `governance_release_policy_ref` | text | да | Optional typed governance refs, если follow-up связан с gate/risk/release policy. Используются только как безопасный контекст; provider-native запись всё равно выполняется через `provider-hub`, а governance decision остаётся у `governance-manager`. |
| `provider_work_item_type` | text | нет | Тип следующего provider-native work item, например `task`, `bug`, `qa`, `release`; может сверяться с `StageTransition.follow_up_type`. |
| `provider_operation_ref` | text | да | Safe ref операции `provider-hub` после dispatch или заранее известная ссылка; сам provider payload, response body и raw error не хранятся. |
| `status` | enum | нет | `planned`, `requested`, `created`, `updated`, `commented`, `review_signaled`, `failed`, `cancelled`. |
| `instruction_body_digest` | text | да | Digest открытых инструкций follow-up без сохранения body. |
| `safe_title` | text | нет | Bounded title для следующей provider-native задачи; не содержит transcript, prompt text, raw provider payload, stdout/stderr/logs или секреты. |
| `safe_summary` | text | да | Bounded summary для события и UI, без больших отчётов и raw payload. |
| `role_hint`, `stage_hint` | text | да | Короткие безопасные подсказки для следующей роли или этапа. |
| `idempotency_key` | text | нет | Сохранённый command idempotency trace: явный `idempotency_key` или command-derived key. |
| `version` | bigint | нет | Версия intent для будущих lifecycle-переходов. |
| `created_at`, `updated_at` | timestamptz | нет | Технические временные метки. |

`FollowUpIntent` не хранит raw prompt, transcript, файлы workspace, большие отчёты, provider response, body будущего `Issue`, тело комментария, тело review signal, inline comment body, тексты руководящих документов, prompt templates или flow files. При dispatch `agent-manager` сначала атомарно резервирует локальный переход bump версии и safe `provider_command:<uuid>` ref, сформированный детерминированно от intent и `FollowUpDispatchKind`. Только после этого формируется bounded safe instruction body для typed вызова `provider-hub.CreateIssue`, `UpdateIssue`, `CreateComment`, `UpdateComment`, `UpdatePullRequest` или `CreateReviewSignal`; в БД остаются `safe_title`, `safe_summary`, digest, статус, `provider_operation_ref`, safe provider result refs и bounded command snapshot без raw body. Для `update_pull_request` target должен совпадать с сохранённым `provider_pull_request_ref`, а команда требует provider expected version. Для `create_review_signal` сохраняется только `provider_review_signal_ref` и статус `review_signaled`; governance approval/release decision остаётся в governance-контуре. Повтор команды с тем же ключом возвращает тот же intent только при совпадении нормализованного payload; отличающийся payload или stale `expected_version` получает безопасный conflict до повторного provider write.

### SelfDeployPlan

`SelfDeployPlan` — авторитетное состояние `agent-manager` для self-deploy orchestration после safe provider/project signal или typed plan input. Он описывает, какие сервисы и категории путей затронуты после merge/push в `main`, какие runtime job types ожидаются после approval, и какие governance refs нужны для решения владельца. После ready-плана `agent-manager` подготавливает governance gate через `governance-manager.PrepareSelfDeployPlanGate`; затем потребляет `governance.gate.resolved` только для target `self_deploy_plan`, сверяет существующие `gate_request_ref`/`gate_decision_ref` при их наличии, дозаписывает отсутствующие governance refs и переводит plan дальше. Если событие уже обработано как `poison` или потеряно до записи refs, стартовая сверка повторно читает существующие risk/gate/decision через safe `governance-manager` read API и восстанавливает тот же переход без ручного checkpoint/replay. Только `approve` и `approve_with_conditions` разрешают build/deploy dispatch; `reject`, `request_changes`/`revise`, `hold`, `rollback` и `escalate` закрывают путь без runtime jobs. После `approved` gate `agent-manager` идемпотентно готовит build context через `runtime-manager.PrepareBuildContext`, получает ready build plan через `project-catalog.GetSelfDeployBuildPlan`, создаёт `JOB_TYPE_BUILD`, после successful build получает checked deploy plan через `project-catalog.GetSelfDeployDeployPlan` и создаёт `JOB_TYPE_DEPLOY`. Если checked policy snapshot плана устарел, `agent-manager` не подставляет latest policy в старый plan: plan становится terminal `failed` с safe code `policy_stale`, а следующий рабочий путь начинается с нового provider/project signal и нового plan. Проектная, provider-native, runtime и governance истина остаются у сервисов-владельцев.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор плана. |
| `scope_type`, `scope_ref` | text | нет | Область plan, обычно repository scope для `codex-k8s/kodex`. |
| `project_ref` | text | нет | Safe ref проекта из `project-catalog`; project-owned данные и `services.yaml` остаются у `project-catalog`. |
| `repository_ref` | text | нет | Safe repository ref; provider-native истина остаётся у `provider-hub`. |
| `provider_slug`, `repository_full_name`, `provider_repository_id` | text | да | Safe provider identity из project-side `SelfDeploySignal`; для signal-oriented self-repo path `provider_slug` и `repository_full_name` обязательны, чтобы runtime materializer мог подготовить context без чтения provider payload. |
| `provider_signal_ref` | text | да | Safe ref provider/project signal, например merge/push signal без webhook body. Для signal-oriented создания поле обязательно и уникально среди непустых значений. |
| `source_ref` | text | нет | Safe ref ветки/источника, например `main` + commit ref. |
| `merge_commit_sha` | text | нет | 40- или 64-символьный hex commit id. |
| `services_yaml_ref` | text | да | Safe ref проверенной версии `services.yaml`; полный YAML не хранится. |
| `services_yaml_digest` | text | нет | `sha256:<hex>` digest проверенной декларации. |
| `affected_service_keys` | text[] | нет | Уникальные service keys/path groups; полный diff не хранится. |
| `path_categories` | enum[] | нет | `services_policy`, `service_source`, `service_config`, `deploy_manifest`, `runtime_config`, `documentation`, `test`, `platform_policy`, `other`. |
| `expected_runtime_job_types` | enum[] | нет | Только `build`, `deploy`, `health_check`; `agent_run` не является self-deploy job type. |
| `governance_risk_assessment_ref`, `governance_gate_request_ref`, `governance_gate_decision_ref`, `governance_release_decision_package_ref`, `governance_release_decision_ref`, `governance_risk_profile_ref`, `governance_gate_policy_ref`, `governance_release_policy_ref` | text | да | Typed governance refs для approval path без decision body. `risk_assessment_ref`, `gate_request_ref` и `gate_decision_ref` заполняются из `PrepareSelfDeployPlanGate`; policy refs приходят из project-side input. |
| `runtime_build_contexts` | jsonb | нет | Ограниченный массив safe refs подготовленного build context: `service_key`, `runtime_build_context_ref`, runtime context status, `build_context_ref`, `build_context_digest`, optional `dockerfile_digest`, materialization fingerprint и fingerprint build plan item. Source archive, workspace path, provider payload и значения секретов не хранятся. |
| `runtime_build_jobs` | jsonb | нет | Ограниченный массив safe refs созданных build jobs: `service_key`, optional `service_ref`, `runtime_job_ref`, optional runtime job status и fingerprint build plan item. `job_input_json`, Kubernetes refs, логи и значения секретов не хранятся. |
| `runtime_build_status` | enum | нет | `not_requested`, `preparing_context`, `blocked`, `requested`, `failed`, `succeeded`. `preparing_context` означает approved plan, для которого policy уже проверена, но runtime-owned build context refs/digest ещё не готовы; `requested` означает, что build jobs уже поставлены или переиспользованы; `succeeded` означает, что все build jobs завершены успешно; `blocked` фиксирует policy conflict, invalid ready build spec, отсутствие approved gate или иной non-ready blocker. При `runtime_build_error_code=policy_stale` plan переводится в terminal `failed` и требует нового checked signal/plan вместо повторного owner approval. |
| `runtime_build_plan_fingerprint` | text | да | Fingerprint ready build plan от `project-catalog`; нужен для replay/conflict и не заменяет `services_yaml_digest`. |
| `runtime_build_error_code`, `runtime_build_summary` | text | да | Короткая безопасная диагностика build dispatch без raw provider payload, diff, YAML, логов, prompt/transcript или секретов. |
| `runtime_deploy_jobs` | jsonb | нет | Ограниченный массив safe refs созданных deploy jobs: `service_key`, optional `service_ref`, `runtime_job_ref`, optional runtime job status и fingerprint deploy plan item. Raw manifests, kubeconfig, Kubernetes events, логи и значения секретов не хранятся. |
| `runtime_deploy_status` | enum | нет | `not_requested`, `blocked`, `requested`, `failed`, `succeeded`. Deploy jobs создаются только после successful build и checked `GetSelfDeployDeployPlan(status=ready)`. |
| `runtime_deploy_plan_fingerprint` | text | да | Fingerprint ready deploy plan от `project-catalog`; нужен для replay/conflict. |
| `runtime_deploy_error_code`, `runtime_deploy_summary` | text | да | Короткая безопасная диагностика deploy dispatch без raw manifests, provider payload, логов, kubeconfig или секретов. |
| `safe_summary` | text | да | Bounded summary для owner/governance review; не содержит raw webhook, diff, YAML, prompt/transcript, логи, секреты или токены. |
| `plan_fingerprint` | text | нет | Детерминированный fingerprint нормализованного plan input. |
| `idempotency_key` | text | нет | Command idempotency trace для replay/conflict. |
| `status` | enum | нет | `pending_approval`, далее `approved`, `rejected`, `cancelled`, `failed` для будущего lifecycle. |
| `version` | bigint | нет | Версия плана для optimistic concurrency будущих переходов. |
| `created_at`, `updated_at` | timestamptz | нет | Технические временные метки. |

Для непустого `provider_signal_ref` действует уникальный индекс. Повтор того же signal ref с тем же `plan_fingerprint` возвращает уже созданный plan, даже если producer пришёл с новым `command_id`; тот же signal ref с другим fingerprint конфликтует, пока владелец сигнала не выпустит новый безопасный signal ref. Автоматический signal path в `agent-manager` создаёт plan только после `project-catalog.GetSelfDeploySignal(status=ready)`; non-ready статусы вроде `needs_services_policy_reconcile` не создают запись `SelfDeployPlan`. Повтор после уже подготовленного governance gate переиспользует сохранённые refs/status и не создаёт второй gate request. Повтор `governance.gate.resolved` с тем же `gate_decision_ref` идемпотентен: он не пишет второй decision и не ставит второй build job. Событие с другим `gate_request_ref`, неизвестным plan target или stale состоянием получает безопасную диагностику и не запускает build. Повтор после `approved` gate и уже созданных build jobs возвращает сохранённые runtime job refs; другой build fingerprint для того же plan конфликтует безопасным статусом до нового plan/signal.

`SelfDeployPlan` запрещает хранение raw webhook body, provider response, полного diff, полного `services.yaml`, prompt, transcript, секретов, токенов, kubeconfig, workspace paths и больших логов. Build jobs создаются только после `approved` governance status/decision ref, готового runtime-owned build context и `project-catalog.GetSelfDeployBuildPlan(status=ready)`: `agent-manager` передаёт `runtime-manager.CreateJob(JOB_TYPE_BUILD)` typed `BuildExecutionSpec` из project-owned build plan, а свободный `job_input_json` остаётся пустым объектом. Если `project-catalog` возвращает `build_context_unavailable` или `build_context_required`, план остаётся в `preparing_context` до повторной проверки после подготовки runtime-owned context refs/digest. Deploy jobs создаются только после successful build и checked `GetSelfDeployDeployPlan(status=ready)`; `runtime-manager` принимает только typed `DeployExecutionSpec`, применяет checked manifest bundle через controlled executor и не читает raw YAML из provider, kubeconfig или значения секретов. Health-check jobs остаются отдельным approval-driven переходом.

### AutomationBinding

`AutomationBinding` связывает flow или роль с событием, расписанием или внешним сигналом.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор привязки. |
| `scope_type` | enum | нет | Область автоматизации. |
| `scope_ref` | text | нет | Внешняя ссылка области. |
| `trigger_kind` | enum | нет | `schedule`, `domain_event`, `provider_signal`, `manual`, `external_callback`. |
| `target_flow_id` | uuid | да | Flow для запуска. |
| `target_role_id` | uuid | да | Роль для запуска без полного flow. |
| `policy` | jsonb | нет | Ограничения, throttling и условия. |
| `status` | enum | нет | `active`, `disabled`, `paused`. |
| `version` | bigint | нет | Оптимистичная конкуренция. |

### CommandResult

`CommandResult` хранит идемпотентный след команд `agent-manager`.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `key` | text | нет | Первичный ключ идемпотентного следа. |
| `command_id` | uuid | да | Идемпотентный ключ команды. |
| `idempotency_key` | text | да | Альтернативный ключ, уникальный в паре `operation + actor`. |
| `actor_type` | text | нет | Тип инициатора команды. |
| `actor_id` | text | нет | Идентификатор инициатора команды. |
| `operation` | text | нет | Имя операции. |
| `aggregate_type` | text | нет | `flow`, `flow_version`, `role_profile`, `prompt_template`, `prompt_template_version`, далее `session`, `run`, `session_state_snapshot`, `acceptance`, `follow_up`, `activity`, `human_gate`, `self_deploy_plan`. |
| `aggregate_id` | uuid | нет | Затронутый агрегат. |
| `result_payload` | jsonb | нет | Безопасный результат повтора. |
| `created_at` | timestamptz | нет | Время первого выполнения. |

Перед возвратом сохранённого результата сервис ищет запись по глобальному `command_id` или по паре `operation + actor + idempotency_key`, загружает фактический aggregate и сверяет его scope или идентификатор с текущим запросом. `command_id` и `idempotency_key` не являются границей авторизации.

### OutboxEvent

`OutboxEvent` фиксируется в одной транзакции с изменением агрегата и публикуется через `platform-event-log`.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор события. |
| `aggregate_type` | text | нет | Тип агрегата. |
| `aggregate_id` | uuid | нет | Идентификатор агрегата. |
| `event_type` | text | нет | Имя события `agent.*`. |
| `schema_version` | int | нет | Версия схемы события. |
| `payload` | jsonb | нет | Минимальная полезная нагрузка. |
| `occurred_at` | timestamptz | нет | Когда случилось изменение. |
| `published_at` | timestamptz | да | Когда событие опубликовано. |
| `attempt_count` | int | нет | Счётчик попыток. |
| `next_attempt_at` | timestamptz | нет | Следующая попытка публикации. |
| `locked_until` | timestamptz | да | Краткая аренда события. |
| `last_error` | text | да | Короткая безопасная ошибка. |

## Связи

- `Flow` владеет `FlowVersion`.
- `FlowVersion` владеет `Stage` и `StageTransition`.
- `RoleProfile` владеет `PromptTemplate` и его версиями.
- `StageRoleBinding` связывает `Stage` и `RoleProfile`.
- `AgentSession` содержит несколько `AgentRun`.
- `AgentRun` фиксирует `FlowVersion`, `Stage`, `RoleProfile` с `role_profile_version` и `role_profile_digest`, `PromptTemplateVersion` с digest и использованные guidance refs.
- Guidance refs в `AgentRun` появляются только после разрешения через `package-hub`: стартовая команда может передать selection hints, а `agent-manager` проверяет scope, активность установки, статус версии и состояние manifest. Runtime refs появляются после подготовки workspace, а `runtime_job_ref` появляется только после принятого `JOB_TYPE_AGENT_RUN` в `runtime-manager`; в БД `agent-manager` сохраняются только `runtime_slot_ref`, `runtime_job_ref`, `runtime_context_ref`, `runtime_workspace_ref`, безопасная summary и статус. Typed `AgentRunExecutionSpec` не становится отдельной моделью хранения `agent-manager`: он детерминированно собирается на момент `CreateJob` из уже сохранённых safe refs, результата `PrepareRuntime`, digest generated context, сервисной ссылки на runner image и safe `CodexSessionExecutionSpec`. Вложенный `CodexSessionExecutionSpec` тоже не хранится в БД `agent-manager`: он собирается из object ref/digest версии prompt, result schema ref/digest из конфигурации, session/workspace snapshot refs, hooks/callback refs, outputs/results и allowed secret refs без значений; сам execution input не пишется в БД и не включается в `agent-run.json`. Runner report не добавляет отдельную таблицу: `ReportAgentRunState` обновляет тот же `AgentRun` после сверки `run_id`/`session_id`/slot/job refs и сохраняет только статус, bounded summary, diagnostic digest и failure code. Состояние `timed_out` хранится как `AgentRun.status=failed` с safe `failure_code`, а `cancelled` использует уже существующий terminal `AgentRun.status=cancelled`.
- В `guidance_refs` запрещено хранить `SKILL.md`, scripts, assets, исходники пакета, полный manifest или секреты; для диагностики сохраняется только bounded policy-safe summary.
- `AgentSessionStateSnapshot` относится к `AgentSession` и опционально к `AgentRun`; `AgentSession.latest_state_snapshot_id` указывает на актуальный снимок.
- `AgentActivity` относится к `AgentSession` и опционально к `AgentRun`; это authoritative safe timeline для будущего UI, а не копия hook payload.
- `AcceptanceResult` и `FollowUpIntent` относятся к `AgentSession`, `AgentRun` и `Stage` и могут нести typed `GovernanceContextRef` для связи с risk/gate/release policy контекстом.
- `HumanGateRequest` хранит owner decision wait/result и typed governance refs, чтобы flow мог продолжиться или завершиться без чтения decision body из `governance-manager`.
- `SelfDeployPlan` относится к project/repository refs и provider/project signal refs. Он хранит pending orchestration state для approval path, но не создаёт runtime jobs и не хранит raw provider/project payload.
- Внутри БД `agent-manager` допустимы внешние ключи между своими таблицами.
- Ссылки на provider, runtime, package, interaction, project и access домены хранятся как внешние идентификаторы без SQL-связей с чужими БД. Workspace paths, kubeconfig, `job_input_json`, prompt/transcript, логи и raw payload внешних сервисов не хранятся в `agent-manager`.

## Индексы и запросы

| Запрос | Нужные индексы |
|---|---|
| Активные сессии по provider-native задаче | частичный unique index `(scope_type, scope_ref, provider_work_item_ref)` для `open`/`waiting` при непустом provider target |
| Запуски по сессии и статусу | `(session_id, status, created_at)` |
| Запуски по flow/stage/role | `(flow_version_id, stage_id, role_profile_id, status)` |
| Последний снимок session state | `(session_id, captured_at DESC)` и `latest_state_snapshot_id` на `AgentSession`. |
| Ожидающие решения или runtime | `(status, updated_at)` для `AgentRun` и `AcceptanceResult`. |
| История действий по session/run | `(session_id, started_at DESC, id DESC)` и `(run_id, started_at DESC, id DESC)`. |
| Tool timeline | `(session_id, activity_kind, started_at DESC, id DESC)`, `(run_id, status, started_at DESC, id DESC)` и частичный индекс по `tool_use_id`. |
| Self-deploy планы | `(project_ref, created_at DESC)`, `(repository_ref, created_at DESC)`, `(provider_signal_ref)` и `(status, created_at DESC)`. |
| Активная версия flow | `(flow_id, status, version)` |
| Активные роли по scope | `(scope_type, scope_ref, status, slug)` |
| Prompt version для роли | `(role_profile_id, prompt_kind, status, version)` |
| Follow-up намерения по статусу | `(status, created_at)` |

## Политика хранения данных

- Полные логи агента не хранятся в БД `agent-manager`; хранится короткая безопасная сводка и ссылки на runtime/provider источники.
- Codex session JSON/JSONL хранится объектом в S3-compatible хранилище; `agent-manager` хранит только ссылку, digest, размер и актуальный указатель на последний снимок.
- Prompt render может храниться как digest и безопасная диагностическая ссылка; полный prompt хранится только если это отдельно согласовано политикой аудита.
- Секреты, токены, сырые provider payload и вложения не попадают в `agent-manager`.
- История `Run`, activity timeline, acceptance и follow-up нужна для аудита и воспроизводимости; retention определяется платформенной политикой после согласования с операционным контуром.

## Миграционные ограничения

- Flow, stage, role и prompt version нельзя менять задним числом для уже созданного `Run`.
- Состояния `Run` и acceptance должны быть расширяемыми через enum migration без потери старых значений.
- Данные других сервисов не копируются в `agent-manager` ради удобства UI; для экранов используются gateway/read-model проекции.

## Апрув

- request_id: `owner-2026-05-12-agent-manager-kickoff`
- Решение: approved
- Комментарий: модель данных домена оркестрации агентов согласована как стартовое целевое состояние.
