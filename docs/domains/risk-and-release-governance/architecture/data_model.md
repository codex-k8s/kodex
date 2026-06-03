---
doc_id: DM-CK8S-RISK-GOVERNANCE-0001
type: data-model
title: kodex — модель данных домена рисков и релизов
status: active
owner_role: SA
created_at: 2026-05-22
updated_at: 2026-05-29
related_issues: [322, 769, 815, 827, 845, 856, 869, 886, 957, 976]
related_prs: []
approvals:
  required: ["Owner"]
  status: pending
  request_id: "owner-2026-05-22-risk-governance-kickoff"
---

# Модель данных: риски и релизы

## TL;DR

- Ключевые сущности: `RiskProfile`, `RiskRule`, `RiskAssessment`, `RiskFactor`, `ReviewSignal`, `GatePolicy`, `GateRequest`, `GateDecision`, `ReleaseDecisionPackage`, `ReleaseDecision`, `ReleaseSafetyState`, `BlockingSignal`.
- Технические агрегаты: `CommandResult`, `OutboxEvent`.
- Основные связи: риск-профиль задаёт правила; оценка риска фиксирует факторы; review signals и blocking signals влияют на gate/release decisions; release safety-loop связан с runtime/provider/project refs.
- Риски миграций: нельзя хранить project policy, provider-native истину, runtime logs, диалоговую доставку, секреты и полный diff в БД `governance-manager`.

## Правило внешних ссылок

`governance-manager` хранит внешние ссылки как typed refs:
- `project_ref`;
- `repository_ref`;
- `service_ref`;
- `provider_work_item_ref`;
- `provider_pull_request_ref`;
- `agent_session_ref`;
- `agent_run_ref`;
- `runtime_job_ref`;
- `runtime_environment_ref`;
- `interaction_thread_ref`;
- `release_line_ref`;
- `release_policy_ref`.

Эти ссылки не являются SQL-связями с БД других сервисов. Источник истины остаётся у сервиса-владельца.

## Сущности

### RiskProfile

`RiskProfile` описывает набор правил риска и gate policy для scope. Он не заменяет проектную политику: scope и привязки к проекту/репозиторию приходят из `project-catalog`.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор риск-профиля. |
| `scope_type` | enum | нет | `platform`, `organization`, `project`, `repository`, `service`, `path`, `api_endpoint`, `database_object`, `secret_area`, `runtime_operation`, `release_line`, `runtime_environment`. |
| `scope_ref` | text | нет | Внешняя ссылка на scope. |
| `slug` | text | нет | Стабильный ключ профиля внутри scope. |
| `display_name` | jsonb | нет | Локализованное название. |
| `description` | jsonb | да | Описание профиля. |
| `status` | enum | нет | `draft`, `active`, `disabled`, `archived`. |
| `active_version` | bigint | да | Активная версия правил. |
| `version` | bigint | нет | Оптимистичная конкуренция метаданных профиля. |
| `created_at`, `updated_at` | timestamptz | нет | Технические временные метки. |

### RiskRule

`RiskRule` является версионируемой частью профиля.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор правила. |
| `risk_profile_id` | uuid | нет | Профиль-владелец. |
| `profile_version` | bigint | нет | Версия профиля, где действует правило. |
| `rule_kind` | enum | нет | `path`, `service`, `api`, `database`, `secret`, `auth`, `runtime_action`, `release`, `automation`, `document`, `custom`. |
| `matcher` | jsonb | нет | Типизированное условие: glob, service key, endpoint, migration path, release line и подобное. |
| `min_risk_class` | enum | нет | `R0`, `R1`, `R2`, `R3`. |
| `required_gate_policy_id` | uuid | да | Gate policy, если правило требует дополнительный gate. |
| `reason_template` | jsonb | нет | Человекочитаемое объяснение с i18n. |
| `status` | enum | нет | `active`, `disabled`. |
| `created_at`, `updated_at` | timestamptz | нет | Технические временные метки. |

### RiskAssessment

`RiskAssessment` фиксирует оценку риска для конкретного перехода, артефакта или release candidate.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор оценки. |
| `target_type` | enum | нет | `transition`, `pull_request`, `release_candidate`, `runtime_job`, `policy_change`, `document`. |
| `target_ref` | text | нет | Внешняя ссылка на оцениваемый объект. |
| `project_ref` | text | да | Проект, если применимо. |
| `repository_ref` | text | да | Репозиторий, если применимо. |
| `agent_run_ref` | text | да | Agent run, если оценка связана с flow. |
| `risk_profile_id` | uuid | да | Локальный risk profile, использованный evaluator. |
| `risk_profile_version` | bigint | да | Immutable-версия risk profile, использованная evaluator. |
| `evaluation_summary` | jsonb | нет | Снимок входного safe classifier summary: changed-file summary ref, typed factors и bounded summaries без raw diff/provider payload/логов/секретов. |
| `evidence_refs` | jsonb | нет | Safe refs на evidence, digest и retention metadata без встраивания больших отчётов. |
| `initial_risk_class` | enum | нет | Автоматически рассчитанный риск. |
| `effective_risk_class` | enum | нет | Текущий риск с учётом факторов, signals и decisions. |
| `status` | enum | нет | `draft`, `active`, `superseded`, `closed`. |
| `explanation` | text | нет | Короткое deterministic explanation для UI/API без секретов. |
| `required_gates` | jsonb | нет | Gate requirements, выведенные из profile rules/gate policies и итогового risk class. |
| `version` | bigint | нет | Оптимистичная конкуренция при пересчёте. |
| `created_at`, `updated_at` | timestamptz | нет | Технические временные метки. |

### RiskFactor

`RiskFactor` объясняет, почему assessment получил такой класс.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор фактора. |
| `risk_assessment_id` | uuid | нет | Оценка-владелец. |
| `source_type` | enum | нет | `policy`, `changed_file`, `service`, `api`, `database`, `secret`, `release`, `runtime`, `review_signal`, `human_decision`. |
| `source_ref` | text | да | Ссылка на правило, signal или внешний объект. |
| `risk_class` | enum | нет | Класс, который даёт фактор. |
| `summary` | text | нет | Безопасное объяснение. |
| `created_at` | timestamptz | нет | Когда фактор записан. |

### ReviewSignal

`ReviewSignal` фиксирует результат роли или человека, который влияет на gate/release readiness.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор signal. |
| `risk_assessment_id` | uuid | да | Оценка риска, если signal уже связан. |
| `target_type` | enum | нет | `pull_request`, `document`, `release_candidate`, `runtime_job`, `postdeploy`, `policy_change`. |
| `target_ref` | text | нет | Provider/runtime/document ref. |
| `role_kind` | enum | нет | `reviewer`, `qa`, `lexical_gatekeeper`, `risk_gatekeeper`, `sre`, `security`, `owner`, `custom`. |
| `author_ref` | text | нет | Actor или agent run. |
| `outcome` | enum | нет | `pass`, `pass_with_notes`, `block`, `request_changes`, `raise_risk`, `informational`. |
| `severity` | enum | нет | `info`, `warning`, `blocking`, `critical`. |
| `confidence` | enum | да | `low`, `medium`, `high`, если применимо. |
| `evidence_refs` | jsonb | нет | Ссылки на комментарии, checks, runtime summary, документы. |
| `summary` | text | нет | Короткая безопасная сводка. |
| `source_fingerprint` | text | нет | Локальный fingerprint normalized `target + role + author_ref + evidence kind/ref identity`, чтобы повторная передача того же provider/agent/interaction signal ref не создавала дубль. |
| `created_at` | timestamptz | нет | Когда signal создан. |

`ReviewSignal` принимает только safe refs от owner-доменов: provider review/comment/check refs из `provider-hub`, agent run/session/acceptance refs из `agent-manager`, interaction decision/callback refs из `interaction-hub` и локальные governance refs. Полный provider payload, diff, prompt/transcript, stdout/stderr, workspace paths, секреты и большие отчёты не сохраняются. Повтор с тем же normalized owner-domain evidence identity set возвращает уже записанный signal; повтор с тем же source fingerprint и другой outcome/severity/summary считается конфликтом фактов. Дубли одного `kind/ref` с разной evidence metadata внутри команды отклоняются как неканонический вход.

### GatePolicy

`GatePolicy` задаёт, какой Human gate или role-driven gate нужен для risk class и scope.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор политики gate. |
| `risk_profile_id` | uuid | да | Профиль, где policy объявлена. |
| `profile_version` | bigint | нет | Версия профиля, где действует policy. |
| `gate_kind` | enum | нет | `product`, `architecture`, `technical`, `qa`, `release`, `postdeploy`, `emergency`, `custom`. |
| `min_risk_class` | enum | нет | Минимальный risk class, где gate обязателен. |
| `required_actor_policy` | jsonb | нет | Требование к человеку, группе, роли или duty scope. |
| `required_signal_kinds` | jsonb | нет | Какие review signals должны быть в evidence. |
| `timeout_policy` | jsonb | да | Reminder/escalation правила, исполняемые через `interaction-hub`. |
| `status` | enum | нет | `active`, `disabled`. |

### GateRequest

`GateRequest` описывает конкретный запрос решения.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор gate. |
| `risk_assessment_id` | uuid | да | Связанная оценка риска. |
| `gate_policy_id` | uuid | да | Policy, которая потребовала gate. |
| `target_type` | enum | нет | `transition`, `merge`, `release`, `postdeploy`, `rollback`, `policy_change`, `document_approval`. |
| `target_ref` | text | нет | Что именно ждёт решения. |
| `interaction_request_ref` | text | да | Ссылка на delivery request в `interaction-hub`. |
| `evidence_package` | jsonb | нет | Безопасный пакет фактов и refs. |
| `status` | enum | нет | `requested`, `delivering`, `awaiting_decision`, `resolved`, `expired`, `cancelled`. |
| `terminal_actor_ref` | text | да | Заполняется для `cancelled` и `expired`; actor ref без копирования membership/access state. |
| `terminal_reason` | text | да | Короткая безопасная причина cancel/expire, без raw provider payload, секретов и логов. |
| `terminal_at` | timestamptz | да | Момент перехода в `cancelled` или `expired`. |
| `created_at`, `updated_at` | timestamptz | нет | Технические временные метки. |

### GateDecision

`GateDecision` фиксирует итог gate.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор решения. |
| `gate_request_id` | uuid | нет | Gate-владелец. |
| `decision_actor_ref` | text | нет | Кто принял решение. |
| `outcome` | enum | нет | `approve`, `approve_with_conditions`, `revise`, `reject`, `hold`, `rollback`, `escalate`. |
| `reason` | text | нет | Обоснование решения. |
| `conditions` | jsonb | да | Условия, follow-up или ограничения. |
| `source_ref` | text | да | Provider review, UI response или external callback ref. |
| `decided_at` | timestamptz | нет | Когда решение принято. |

### ReleaseDecisionPackage

`ReleaseDecisionPackage` фиксирует снимок evidence перед release decision.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор пакета. |
| `release_candidate_ref` | text | нет | Кандидат релиза. |
| `project_ref` | text | нет | Проект. |
| `repository_ref` | text | да | Основной repository ref из `project-catalog`, если выделен отдельно. |
| `service_ref` | text | да | Service ref из `project-catalog`, если release package scoped to service. |
| `branch_rules_ref` | text | да | Branch rules ref из `project-catalog`; содержимое policy не копируется. |
| `repository_refs` | text[] | нет | Репозитории в релизе. |
| `release_policy_ref` | text | да | Релизная политика из `project-catalog`. |
| `release_line_ref` | text | да | Релизная линия из `project-catalog`. |
| `risk_assessment_id` | uuid | да | Оценка риска релиза. |
| `provider_refs` | jsonb | нет | Issue/PR/MR/check/review/tag/branch refs без raw provider payload. |
| `runtime_refs` | jsonb | нет | Build/deploy/job/postdeploy refs и короткие безопасные сводки без логов/stdout/stderr. |
| `agent_context` | jsonb | нет | Run/session/stage/acceptance refs без prompt, transcript или workspace paths. |
| `review_signal_ids` | uuid[] | нет | Локальные review signals, включённые в пакет. |
| `evidence_refs` | jsonb | нет | Safe refs/digests/summaries на evidence, без больших отчётов. |
| `integration_refs` | jsonb | нет | Явные безопасные refs соседних доменов: `domain`, `kind`, `ref`, опциональные `status`, `summary`, `digest`, `observed_at`, `version`, `error_code`; локальные governance refs получают ограниченное обогащение. |
| `known_limitations_summary` | text | нет | Короткая safe summary осознанных ограничений и accepted risk. |
| `status` | enum | нет | `draft`, `ready`, `decision_requested`, `closed`. |
| `created_at`, `updated_at` | timestamptz | нет | Технические временные метки. |

`integration_refs` связывают release package с project/repository/release line refs, provider Issue/PR/check/review refs, agent run/acceptance refs, runtime job/deploy refs, local risk assessment refs и gate refs. `governance-manager` валидирует и обогащает только локальные governance refs: для найденных assessment/signal/gate/package refs сохраняются bounded `status`, короткий `summary`, `digest`, `observed_at` и `version` там, где у локального aggregate есть версия. Если вызывающая сторона передала локальный snapshot, который конфликтует с текущим governance state, package build отклоняется. Для project/provider/agent/runtime refs сервис не читает соседние сервисы напрямую: explicit ref сохраняется, а при отсутствии owner-domain summary добавляется safe diagnostic `explicit_ref_unvalidated` в `summary` без raw details. Для audit snapshot refs нормализуются в canonical order по `domain/kind/ref`; полностью одинаковые дубли схлопываются, а дубли с разными `status`, `summary`, `digest`, `observed_at`, `version` или `error_code` отклоняются как конфликтующие факты.

`RecordReleaseRuntimeEvidence` добавляет runtime/deploy evidence к уже созданному release package без новой таблицы: команда обновляет `runtime_refs`, `evidence_refs` и `integration_refs`, требует `expected_version`, не меняет `closed` package и публикует только безопасное событие `governance.release_decision_package.runtime_evidence_recorded`. Для `runtime` refs с `kind=job|deploy|postdeploy` статус ограничен lifecycle-набором `pending`, `claimed`, `running`, `succeeded`, `failed`, `cancelled`, `timed_out`; повтор с тем же fingerprint/digest идемпотентен, конфликтующий digest для того же `domain/kind/ref` отклоняется, а более старый status-снимок не перезаписывает уже сохранённый факт. `GetReleaseDecisionPackage` и `ListReleaseDecisionPackages` читают тот же безопасный снимок для интерфейса владельца и персонала: runtime job/deploy/postdeploy refs, status, короткий безопасный `summary`, `error_code`, `observed_at`, digest, version, связь с gate refs через `integration_refs` и `release_candidate_ref`. Исходное состояние runtime job, deploy-артефакты, логи, Kubernetes objects и полный postdeploy-отчёт остаются у `runtime-manager` или внешнего владельца артефакта; governance хранит только refs, ограниченный статус, `error_code`, digest/version и короткую сводку.

`RecordReleaseAgentEvidence` добавляет agent acceptance/review/runtime evidence к уже созданному release package без новой таблицы: команда обновляет `agent_context`, `evidence_refs` и `integration_refs`, требует `expected_version`, не меняет `closed` package и публикует только безопасное событие `governance.release_decision_package.agent_evidence_recorded`. Для `agent` refs статус ограничен lifecycle-наборами `acceptance`, `run`, `human_gate` и `session`; более старый status-снимок для того же `domain/kind/ref` отклоняется, конфликтующий digest/version/status/summary считается конфликтом, повтор того же fingerprint идемпотентен. `GetReleaseDecisionPackage` и `ListReleaseDecisionPackages` читают тот же безопасный снимок: agent session/run/stage/acceptance/human gate refs, runtime job refs, локальные review/gate refs, status, короткий `summary`, `observed_at`, digest, version и версию package. Prompt body, transcript, raw tool input/output, stdout/stderr, runtime logs, workspace paths, секреты и БД `agent-manager` не попадают в `governance-manager`.

Входящий consumer `agent.acceptance.completed`/`agent.acceptance.failed` является тонким способом вызвать тот же `RecordReleaseAgentEvidence`, когда событие `agent-manager` уже несёт явный `governance_release_decision_package_ref`. Он не создаёт новый тип хранения и не ищет package по project/run: отсутствие package ref подтверждается без записи, некорректная ссылка или конфликтующий fingerprint фиксируется как permanent diagnostic. В release package попадают только acceptance/session/run/stage refs, runtime job ref, status, короткая сводка, digest, `observed_at`, version и event idempotency fingerprint.

### GovernanceSummary

`GovernanceSummary` не является отдельной таблицей. Это безопасная модель чтения, которую `governance-manager` собирает из локальных risk assessment, review signal, gate, release package/decision, blocking signal и safety-loop state. Scope обязателен и содержит ровно один selector: `target`, `project_context`, `release_candidate_ref`, `release_decision_package_id` или `integration_ref` из release package. Смешанные selectors отклоняются как `invalid_argument`, потому что summary должен описывать один понятный owner-context и не объединять unrelated decisions/evidence. Для `integration_ref` используется уже сохранённый `integration_refs` snapshot, поэтому summary может находить package по provider PR/check, agent run/acceptance или runtime job ref без чтения БД соседних сервисов.

Ответ делится на `pending_decisions`, `completed_decisions` и `evidence_summaries`. В decision item попадают только типизированные статусы и refs: `risk_class`, `required_gate_count`, review outcome/severity, gate request status, gate outcome, release package status, release decision status/outcome, blocking signal status, release candidate/package refs, provider/runtime/agent refs, timestamps, version и короткий `safe_summary`. Evidence summary хранит только `source_kind`, `source_ref`, status/outcome, digest/version, `observed_at`, `error_code` и bounded summary. Если связанный локальный risk/review/gate ref отсутствует, summary возвращает partial response с безопасной диагностикой, а не падает и не делает implicit lookup по project/run/provider payload. Raw diff, provider payload, prompt/transcript, stdout/stderr, workspace paths, Kubernetes payload, секреты и большие логи в эту модель не попадают.

Для self-deploy summary дополнительно показывает `status.pending_required_gate_count` и `next_action_code=request_governance_gate`, когда risk assessment уже требует owner/governance gate, а локальный gate request ещё не создан. Это позволяет manager-агенту или будущей операторской поверхности показать безопасный следующий шаг без вычисления правил вне `governance-manager`.

Для реального `SelfDeployPlan` используется target `self_deploy_plan` с ref на plan id из `agent-manager`. Команда `PrepareSelfDeployPlanGate` сохраняет risk assessment и gate request на этом target, а `plan_fingerprint` кладётся только как digest safe `EvidenceRef(kind=self_deploy_plan, ref=<plan_ref>)`. Повтор той же доставки с тем же fingerprint возвращает существующие assessment/gate/decision refs, а новый fingerprint для того же `self_deploy_plan` target считается конфликтом. Модель хранит service keys, path categories, expected runtime job types, `services.yaml` digest/ref, provider/source refs и короткую summary как bounded factors/evidence; полный diff, webhook body, provider response, полный `services.yaml`, runtime logs, prompt/transcript, kubeconfig и секреты не принимаются.

### ReleaseDecision

`ReleaseDecision` фиксирует go/no-go, hold, rollback или follow-up.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор решения. |
| `release_decision_package_id` | uuid | нет | Пакет подтверждений. |
| `gate_decision_id` | uuid | да | Связанный gate, если был нужен человек. |
| `outcome` | enum | нет | `go`, `go_with_conditions`, `no_go`, `hold`, `rollback`, `follow_up_required`. |
| `decision_actor_ref` | text | нет | Человек или policy automation. |
| `decision_policy_ref` | text | да | Версия policy/evaluator; обязательна для автоматического решения. |
| `reason` | text | нет | Обоснование решения. |
| `conditions` | jsonb | да | Условия релиза или follow-up. |
| `status` | enum | нет | `requested`, `resolved`, `cancelled`. |
| `version` | bigint | нет | Оптимистичная конкуренция. |
| `decided_at` | timestamptz | нет | Когда решение принято. |
| `created_at`, `updated_at` | timestamptz | нет | Технические временные метки. |

### ReleaseSafetyState

`ReleaseSafetyState` ведёт postdeploy safety-loop.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор состояния. |
| `release_decision_package_id` | uuid | нет | Релизный пакет. |
| `current_state` | enum | нет | `release_candidate`, `awaiting_release_gate`, `deploying`, `postdeploy_observation`, `stable`, `hold`, `rollback`, `follow_up_required`. |
| `runtime_job_ref` | text | да | Текущий deploy/postdeploy job. |
| `blocking_signal_count` | int | нет | Количество активных blocking signals. |
| `last_state_reason` | text | нет | Короткое объяснение последнего перехода. |
| `version` | bigint | нет | Оптимистичная конкуренция. |
| `created_at`, `updated_at` | timestamptz | нет | Технические временные метки. |

### BlockingSignal

`BlockingSignal` описывает сигнал, который останавливает переход или релиз.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор сигнала. |
| `target_type` | enum | нет | `risk_assessment`, `gate`, `release`, `postdeploy`, `runtime_job`. |
| `target_ref` | text | нет | Target или агрегат governance. |
| `source_type` | enum | нет | `acceptance`, `review_signal`, `runtime`, `provider`, `interaction`, `human`, `monitoring`. |
| `source_ref` | text | да | Ссылка на первоисточник. |
| `severity` | enum | нет | `warning`, `blocking`, `critical`. |
| `summary` | text | нет | Безопасное объяснение. |
| `status` | enum | нет | `active`, `resolved`, `dismissed`. |
| `created_at`, `resolved_at` | timestamptz | да | Временные метки. |

### CommandResult

`CommandResult` хранит идемпотентный след команд `governance-manager`.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `key` | text | нет | Первичный ключ идемпотентного следа. |
| `command_id` | uuid | да | Идемпотентный ключ команды. |
| `idempotency_key` | text | да | Альтернативный ключ в паре `operation + actor`. |
| `operation` | text | нет | Имя операции. |
| `actor_ref` | text | нет | Инициатор команды. |
| `aggregate_type` | text | нет | Тип агрегата. |
| `aggregate_id` | uuid | нет | Идентификатор агрегата. |
| `result_payload` | jsonb | нет | Безопасный результат повтора. |
| `created_at` | timestamptz | нет | Когда команда впервые выполнена. |

### OutboxEvent

`OutboxEvent` фиксируется в одной транзакции с изменением агрегата и публикуется через `platform-event-log`.

| Поле | Тип | Может быть пустым | Примечание |
|---|---|---:|---|
| `id` | uuid | нет | Идентификатор события. |
| `aggregate_type` | text | нет | Тип агрегата. |
| `aggregate_id` | uuid | нет | Идентификатор агрегата. |
| `event_type` | text | нет | Имя события `governance.*`. |
| `schema_version` | int | нет | Версия схемы события. |
| `payload` | jsonb | нет | Минимальная безопасная полезная нагрузка. |
| `occurred_at` | timestamptz | нет | Время доменного изменения. |
| `published_at` | timestamptz | да | Заполняется после публикации. |
| `attempt_count` | int | нет | Число попыток. |
| `next_attempt_at` | timestamptz | нет | Следующая попытка. |
| `locked_until` | timestamptz | да | Краткая аренда. |
| `last_error` | text | да | Короткая безопасная ошибка. |

## Связи

- `RiskProfile` владеет `RiskRule` и версионированными `GatePolicy`.
- `RiskAssessment` владеет набором `RiskFactor`.
- `ReviewSignal` может повышать `RiskAssessment` и становиться частью `GateRequest.evidence_package`.
- `GateRequest` может иметь один финальный `GateDecision`; повторные callbacks записываются через идемпотентность и аудит.
- `ReleaseDecisionPackage` связывает `RiskAssessment`, `ReviewSignal`, provider refs, runtime refs и project/release refs.
- `ReleaseDecision` ссылается на `ReleaseDecisionPackage` и опционально на `GateDecision`.
- `ReleaseSafetyState` ведёт состояние postdeploy вокруг `ReleaseDecisionPackage`.
- `BlockingSignal` может относиться к risk assessment, gate, release package, safety state или runtime job ref.
- Внутри БД `governance-manager` допустимы внешние ключи только между своими таблицами.
- Ссылки на project, provider, agent, runtime, interaction и access домены хранятся как внешние идентификаторы.

## Индексы и запросы

| Запрос | Нужные индексы |
|---|---|
| Активные risk profiles по scope | `(scope_type, scope_ref, status, slug)` |
| Правила активной версии профиля | `(risk_profile_id, profile_version, status, rule_kind)` |
| Gate policies активной версии профиля | `(risk_profile_id, profile_version, status, gate_kind)` |
| Assessment по target | `(target_type, target_ref, status)` |
| Assessment по проекту и классу риска | `(project_ref, effective_risk_class, status, updated_at)` |
| Signals по target | `(target_type, target_ref, created_at)` |
| Blocking signals | `(status, severity, created_at)` |
| Ожидающие gates | `(status, updated_at)` where status in `requested`, `delivering`, `awaiting_decision` |
| Gate по assessment | `(risk_assessment_id, updated_at, id)` where `risk_assessment_id is not null` |
| Release package по candidate | `(release_candidate_ref, status)` |
| Safety state по release package | `(release_decision_package_id, current_state)` |
| Непубликованные события | `(published_at, occurred_at)` where `published_at is null` |
| Идемпотентный след команд | `(command_id)` unique и `(operation, actor_ref, idempotency_key)` unique where key present |

## Политика хранения данных

- Risk assessments, gate decisions и release decisions хранятся как audit-critical записи.
- Evidence package хранит refs и безопасные summaries, а не полный diff, сырые payload, секреты или полные logs.
- Старые версии risk profiles, rules и gate policies хранятся для воспроизводимости решений.
- Review signals могут ссылаться на provider comments/reviews/checks, но не копируют полный provider artifact.
- Runtime refs указывают на `job` и короткий summary; полные logs остаются у runtime/Kubernetes/logging stack.
- Interaction refs указывают на delivery/callback thread; диалоговая история остаётся у `interaction-hub`.

## Миграционные ограничения

- Нельзя менять задним числом risk factors и decisions, которые уже разрешили transition или release.
- Изменение risk profile не меняет историю старых assessments; новая версия применяется только к новым или явно пересчитанным assessments.
- Enum состояния должны расширяться без удаления старых значений.
- Любой future backfill должен сохранять исходный `created_at`/`decided_at` и источник миграционного решения.

## Апрув

- request_id: `owner-2026-05-22-risk-governance-kickoff`
- Решение: pending
- Комментарий: модель данных описывает целевой контур `governance-manager`; MVP-миграции и storage-основа покрывают только persistency-срез без полного evaluator и release engine.
