---
doc_id: DM-CK8S-AGENT-ORCHESTRATION-0001
type: data-model
title: kodex — модель данных домена оркестрации агентов
status: active
owner_role: SA
created_at: 2026-05-12
updated_at: 2026-05-26
related_issues: [733, 749, 759, 772, 322, 782, 795, 809, 820, 834]
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

- Ключевые сущности: `Flow`, `FlowVersion`, `Stage`, `StageTransition`, `RoleProfile`, `StageRoleBinding`, `PromptTemplate`, `PromptTemplateVersion`, `AgentSession`, `AgentRun`, `AgentSessionStateSnapshot`, `AgentActivity`, `AcceptanceCheck`, `AcceptanceResult`, `FollowUpIntent`, `AutomationBinding`.
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
| `runtime_ref` | text/json | да | Безопасные refs runtime: slot/workspace/context или fingerprint подготовки без локальных workspace paths. |
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

`AcceptanceCheck` описывает тип проверки в policy/flow-контексте, а `AcceptanceResult` является хранимым агрегатом результата. Базовый lifecycle создаёт один pending result на команду `RequestAcceptance`, затем `RecordAcceptanceResult` переводит его в `passed`, `failed`, `waiting` или `skipped` через ожидаемую версию. Для `human_gate` доступна только фиксация ожидания `waiting` с безопасной ссылкой на gate/risk/governance; финальное решение остаётся в сервисе-владельце.

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
| `version` | bigint | нет | Оптимистичная конкуренция результата приёмки. |
| `created_at`, `updated_at` | timestamptz | нет | Технические временные метки. |

`details_json` не является отчётом QA runner и не хранит raw provider payload, workspace files, prompt text, flow files, руководящие документы, stdout/stderr/logs, секреты, токены или PII. Если приёмка ждёт Human gate или governance decision, `agent-manager` фиксирует только статус ожидания и безопасные `gate_ref`/`risk_ref`/`governance` refs; само решение хранит сервис-владелец.

### FollowUpIntent

`FollowUpIntent` описывает намерение создать или обновить provider-native задачу следующего этапа. В `agent-manager` это авторитетное состояние intent, а не результат provider write: создание `Issue`, комментария или `PR/MR` выполняет `provider-hub` отдельной командой в следующем интеграционном срезе.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор намерения. |
| `session_id` | uuid | нет | Сессия. |
| `run_id` | uuid | да | `Run`, результат которого породил follow-up. Если указан `AcceptanceResult`, связь с `Run` должна совпадать. |
| `from_stage_id` | uuid | да | Исходный этап. |
| `to_stage_id` | uuid | да | Следующий этап. |
| `acceptance_result_id` | uuid | да | Положительный результат machine acceptance, если follow-up создаётся по итогам приёмки. Pending/failed/waiting acceptance не может породить intent. |
| `provider_work_item_ref`, `provider_pull_request_ref`, `provider_comment_ref`, `provider_review_signal_ref` | text | да | Безопасные provider refs. Хотя бы один target ref обязателен; значения имеют safe-ref формат `kind:value`, ограничены по длине и не содержат raw/log/secret markers. |
| `provider_work_item_type` | text | нет | Тип следующего provider-native work item, например `task`, `bug`, `qa`, `release`; может сверяться с `StageTransition.follow_up_type`. |
| `provider_operation_ref` | text | да | Ссылка на будущую или уже известную операцию `provider-hub`; сам provider payload не хранится. |
| `status` | enum | нет | `planned`, `requested`, `created`, `failed`, `cancelled`. |
| `instruction_body_digest` | text | да | Digest открытых инструкций follow-up без сохранения body. |
| `safe_title` | text | нет | Bounded title для следующей provider-native задачи; не содержит transcript, prompt text, raw provider payload, stdout/stderr/logs или секреты. |
| `safe_summary` | text | да | Bounded summary для события и UI, без больших отчётов и raw payload. |
| `role_hint`, `stage_hint` | text | да | Короткие безопасные подсказки для следующей роли или этапа. |
| `idempotency_key` | text | нет | Сохранённый command idempotency trace: явный `idempotency_key` или command-derived key. |
| `version` | bigint | нет | Версия intent для будущих lifecycle-переходов. |
| `created_at`, `updated_at` | timestamptz | нет | Технические временные метки. |

`FollowUpIntent` не хранит raw prompt, transcript, файлы workspace, большие отчёты, provider response, body будущего `Issue`, тексты руководящих документов, prompt templates или flow files. Повтор команды с тем же ключом возвращает тот же intent только при совпадении нормализованного payload; отличающийся payload получает безопасный conflict.

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
| `aggregate_type` | text | нет | `flow`, `flow_version`, `role_profile`, `prompt_template`, `prompt_template_version`, далее `session`, `run`, `session_state_snapshot`, `acceptance`, `follow_up`, `activity`. |
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
- Guidance refs в `AgentRun` появляются только после разрешения через `package-hub`: стартовая команда может передать selection hints, а `agent-manager` проверяет scope, активность установки, статус версии и состояние manifest. Runtime refs появляются только после подготовки workspace в `runtime-manager`.
- В `guidance_refs` запрещено хранить `SKILL.md`, scripts, assets, исходники пакета, полный manifest или секреты; для диагностики сохраняется только bounded policy-safe summary.
- `AgentSessionStateSnapshot` относится к `AgentSession` и опционально к `AgentRun`; `AgentSession.latest_state_snapshot_id` указывает на актуальный снимок.
- `AgentActivity` относится к `AgentSession` и опционально к `AgentRun`; это authoritative safe timeline для будущего UI, а не копия hook payload.
- `AcceptanceResult` и `FollowUpIntent` относятся к `AgentSession`, `AgentRun` и `Stage`.
- Внутри БД `agent-manager` допустимы внешние ключи между своими таблицами.
- Ссылки на provider, runtime, package, interaction, project и access домены хранятся как внешние идентификаторы без SQL-связей с чужими БД.

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
