---
doc_id: DM-CK8S-CODEX-HOOK-INGRESS-0001
type: data-model
title: codex-hook-ingress - модель данных и состояния
status: active
owner_role: SA
created_at: 2026-05-22
updated_at: 2026-05-26
related_issues: [698, 753, 778, 786, 793, 808, 322]
related_prs: []
approvals:
  required: ["Owner"]
  status: pending
  request_id: "owner-2026-05-22-codex-hook-ingress-docs"
---

# Модель данных и состояния codex-hook-ingress

## TL;DR

- Ключевые сущности: `HookSourceBinding`, `HookEventEnvelope`, `HookDeliveryAttempt`, `HookDecisionBridge`, `HookSanitizationReport`, `HookOperationalEvent`; CHI-2 добавляет edge value objects для hook emitter/local sidecar без новой доменной БД.
- Основные связи: hook event связывается с `run_id`, `session_id`, `slot_id`, `turn_id`, scope, route и correlation id, но не становится владельцем `Run`, slot, provider artifact, dialogue или skill.
- Риски миграций: нельзя хранить raw hook input, raw tool input/output, stdout/stderr, prompt, transcript, session dump, secret values или `SKILL.md` content.

## Принцип владения

`codex-hook-ingress` может хранить только service-local состояние входной границы: idempotency, delivery attempts, short operational history, audit-safe sanitizer facts и временный decision bridge. Доменная истина остаётся у владельцев:

| Данные | Владелец |
|---|---|
| `Run`, agent session, flow/stage/role, ожидание flow | `agent-manager` |
| Risk/gate request, decision, policy-based approval | `governance-manager` |
| Slot, workspace, runtime job, materialized skills | `runtime-manager` |
| Provider artifacts, projections, limits, reconciliation | `provider-hub` |
| Dialogues, notifications, owner feedback delivery | `interaction-hub` |
| Package source/version/install/manifest | `package-hub` |
| Skill selection policy and run metadata | `agent-manager` |
| MCP tool discovery and calls | `platform-mcp-server` |

## Entity: HookSourceBinding

Назначение: проверенная привязка emitter/sidecar к actor, run, session, slot и scope. Источник правды находится в `agent-manager` и `runtime-manager`; ingress может хранить короткоживущую проекцию или cache для быстрой проверки.

Важные инварианты:

- Binding не выдаёт новых прав, а только подтверждает уже выданный runtime context.
- Binding имеет TTL не дольше жизни slot/session token.
- Binding не хранит token value, kubeconfig, provider credential или secret.

| Field | Type | Nullable | Default | Constraints | Notes |
|---|---|---:|---|---|---|
| `binding_id` | uuid | нет | generated | PK | Service-local id. |
| `source_ref` | text | нет |  | indexed | Идентификатор emitter/sidecar без секрета. |
| `actor_ref` | text | нет |  | indexed | Principal или service account ref. |
| `organization_id` | uuid | нет |  | indexed | Scope. |
| `project_id` | uuid | нет |  | indexed | Scope. |
| `repository_id` | uuid | да | null | indexed | Scope, если событие связано с репозиторием. |
| `run_id` | uuid | нет |  | indexed | Ссылка на `agent-manager`. |
| `session_id` | text | нет |  | indexed | Codex session id или platform session ref. |
| `slot_id` | uuid | нет |  | indexed | Ссылка на `runtime-manager`. |
| `role_ref` | text | да | null |  | Role/stage context от `agent-manager`. |
| `stage_ref` | text | да | null |  | Role/stage context от `agent-manager`. |
| `capability_context_id` | uuid | да | null |  | Ref выбранного capability set, если есть. |
| `expires_at` | timestamptz | нет |  | indexed | TTL binding. |
| `status` | enum | нет | `active` | `active`, `revoked`, `expired` | Локальный статус проекции. |

## Entity: HookEventEnvelope

Назначение: нормализованное и очищенное событие для idempotency, короткой истории и маршрутизации. Это не raw input Codex.

Важные инварианты:

- `hook_event_name` только из MVP-набора.
- `payload_json` содержит только sanitized fields.
- Размер `payload_json` после нормализации не больше policy limit.
- High-frequency allow-события могут не сохраняться полностью и жить только в operational feed.

| Field | Type | Nullable | Default | Constraints | Notes |
|---|---|---:|---|---|---|
| `event_id` | uuid | нет |  | PK | Идемпотентность события. |
| `schema_version` | text | нет |  |  | Версия envelope. |
| `hook_event_name` | enum | нет |  | indexed | `SessionStart`, `UserPromptSubmit`, `PreToolUse`, `PermissionRequest`, `PostToolUse`, `Stop`. |
| `event_time` | timestamptz | нет |  | indexed | Время runtime. |
| `received_at` | timestamptz | нет | now | indexed | Время ingress. |
| `binding_id` | uuid | нет |  | FK local | Проверенный source binding. |
| `run_id` | uuid | нет |  | indexed | Ссылка на `agent-manager`. |
| `session_id` | text | нет |  | indexed | Session ref. |
| `slot_id` | uuid | нет |  | indexed | Slot ref. |
| `turn_id` | text | да | null | indexed | Turn-scoped events. |
| `tool_name` | text | да | null |  | Safe canonical name, если есть. |
| `tool_category` | enum | да | null | `shell`, `patch`, `mcp`, `other` | Категория без raw input. |
| `tool_use_id` | text | да | null |  | Tool call id. |
| `safe_summary` | text | да | null | max 4 KiB | Без секретов. |
| `payload_digest` | text | нет |  |  | Digest нормализованных значимых частей. |
| `payload_json` | jsonb | нет | `{}` | max 64 KiB | Только sanitized payload. |
| `capability_context_id` | uuid | да | null |  | Ref выбранного skill/capability set. |
| `skill_refs_json` | jsonb | да | null | bounded | Только refs/digests, без `SKILL.md`. |
| `correlation_id` | text | нет |  | indexed | Сквозная корреляция. |
| `retention_class` | enum | нет | `operational` | `audit`, `operational`, `realtime` | Управляет сроком хранения. |

## Entity: HookDeliveryAttempt

Назначение: локальная запись попытки доставки safe event владельцу.

Важные инварианты:

- Delivery attempt не содержит raw payload.
- Повтор не меняет `event_id` и `payload_digest`.

| Field | Type | Nullable | Default | Constraints | Notes |
|---|---|---:|---|---|---|
| `attempt_id` | uuid | нет | generated | PK |  |
| `event_id` | uuid | нет |  | FK local, indexed |  |
| `route` | enum | нет |  | `agent-manager`, `runtime-manager`, `provider-hub`, `governance-manager`, `interaction-hub`, `operations` | Downstream route. |
| `attempt_no` | int | нет | 1 |  | Retry count. |
| `status` | enum | нет | `pending` | `pending`, `delivered`, `retrying`, `failed`, `dropped` |  |
| `last_error_code` | text | да | null |  | Error class без payload. |
| `next_retry_at` | timestamptz | да | null | indexed | Backoff. |
| `created_at` | timestamptz | нет | now |  |  |
| `updated_at` | timestamptz | нет | now |  |  |

## Entity: HookDecisionBridge

Назначение: временное состояние синхронного bridge для `PermissionRequest` или policy-controlled `PreToolUse`.

Важные инварианты:

- Gate/decision принадлежит `governance-manager`, не ingress.
- Ожидание flow принадлежит `agent-manager` и передаётся как ref, если нужно продолжить агентный процесс после решения.
- После timeout bridge закрывается безопасным результатом.
- Decision payload не содержит raw command или secret.

| Field | Type | Nullable | Default | Constraints | Notes |
|---|---|---:|---|---|---|
| `bridge_id` | uuid | нет | generated | PK |  |
| `event_id` | uuid | нет |  | FK local, indexed | Permission/pre-tool event. |
| `owner_request_ref` | text | да | null | indexed | Ссылка на gate/request у `governance-manager` или flow-wait ref у `agent-manager`. |
| `status` | enum | нет | `waiting` | `waiting`, `allowed`, `denied`, `no_decision`, `timed_out`, `failed` | Local bridge status; `no_decision` не является Codex `permissionDecision: "ask"`. |
| `risk_class` | enum | нет | `unknown` | `low`, `medium`, `high`, `unknown` | Safe classification. |
| `decision_reason` | text | да | null | max 4 KiB | Sanitized. |
| `expires_at` | timestamptz | нет |  | indexed | Timeout. |
| `resolved_at` | timestamptz | да | null |  |  |

## Entity: HookSanitizationReport

Назначение: audit-safe факт очистки или отказа payload.

Важные инварианты:

- Report не хранит найденное значение секрета.
- Для rejected payload хранится только причина, size class, field path class и digest.

| Field | Type | Nullable | Default | Constraints | Notes |
|---|---|---:|---|---|---|
| `report_id` | uuid | нет | generated | PK |  |
| `event_id` | uuid | да | null | indexed | Может отсутствовать, если событие отклонено до event id. |
| `result` | enum | нет |  | `accepted`, `truncated`, `redacted`, `rejected` |  |
| `reason_code` | text | нет |  | indexed | Например `secret_like`, `payload_too_large`, `forbidden_field`. |
| `field_class` | text | да | null |  | Например `tool_input`, `stdout`, `prompt`, без значения. |
| `size_bytes` | int | да | null |  | Размер после нормализации или входа. |
| `created_at` | timestamptz | нет | now | indexed |  |

## Entity: HookOperationalEvent

Назначение: короткая realtime/ops лента для UI и восстановления экрана после переподключения.

Важные инварианты:

- Лента не является аудитом всех событий платформенного MVP-набора.
- Retention короткий и задаётся policy.
- Запись содержит только safe summary.

| Field | Type | Nullable | Default | Constraints | Notes |
|---|---|---:|---|---|---|
| `operation_event_id` | uuid | нет | generated | PK |  |
| `event_id` | uuid | да | null | indexed | Ссылка на HookEventEnvelope, если сохранён. |
| `run_id` | uuid | нет |  | indexed |  |
| `session_id` | text | нет |  | indexed |  |
| `slot_id` | uuid | да | null | indexed |  |
| `event_kind` | text | нет |  | indexed | UI-friendly kind. |
| `severity` | enum | нет | `info` | `debug`, `info`, `warn`, `error` |  |
| `summary` | text | нет |  | max 4 KiB | Safe text. |
| `route` | text | да | null |  | Downstream route. |
| `created_at` | timestamptz | нет | now | indexed |  |
| `expires_at` | timestamptz | нет |  | indexed | Retention. |

## Value object: CapabilityContextRef

Назначение: ссылка на выбранный набор skills/capabilities для run. Не является сущностью hook ingress.

| Field | Meaning |
|---|---|
| `capability_context_id` | Идентификатор выбранного набора у `agent-manager`. |
| `selected_by` | Ref решения `agent-manager`. |
| `materialized_by` | Ref materialization у `runtime-manager`. |
| `scope` | Platform, organization, project, repository, flow, stage или role. |
| `skill_refs` | Массив refs: source kind, package installation ref, version, digest, invocation policy. |
| `workspace_ref` | Ref на materialized path или workspace mount без локального raw path, если path чувствителен. |

`codex-hook-ingress` может копировать этот value object в sanitized event только как refs/digests. Тексты `SKILL.md`, scripts, references, assets и package manifest не хранятся здесь.

## Edge value object: HookEmitterRuntimeConfig

Назначение: machine-readable runtime policy для hook emitter/local sidecar. Source of truth: `specs/jsonschema/codex-hook-ingress.v1/hook-emitter-config.v1.schema.json`.

Это не таблица `codex-hook-ingress`: config выдаётся или материализуется runtime-контуром рядом со slot и валидируется до запуска emitter/sidecar.

| Field | Meaning |
|---|---|
| `runtime_role` | `hook_emitter` или `local_sidecar`. |
| `codex_hook_input` | Command hook читает JSON object из `stdin`; transcript path не читается и не пересылается. |
| `supported_hook_events` | Ровно `SessionStart`, `UserPromptSubmit`, `PreToolUse`, `PermissionRequest`, `PostToolUse`, `Stop`. |
| `internal_session_events` | Будущие compact/session checkpoints как внутренние события платформы, не Codex hooks. |
| `delivery_contract` | Логический receiver `codex-hook-ingress`, operation `SubmitHookEvent`, transport profile `transport_tbd_internal_command`. |
| `auth_policy` | Source binding, workload identity/short-lived token/mTLS и запрет secret material в config. |
| `retry_policy` | Backoff, jitter, max attempts и список non-retryable ошибок. |
| `buffer_policy` | Лимиты count/bytes/TTL, encrypted local spool и запрет raw payload storage. |
| `backpressure_policy` | Разделение audit-critical, decision bridge и realtime-only событий. |

## Edge value object: LocalSidecarBufferEntry

Назначение: временная запись локального buffer в slot runtime. Она не является канонической записью ingress и не заменяет `HookDeliveryAttempt`.

| Field | Meaning |
|---|---|
| `event_id` | Идемпотентный id envelope. |
| `payload_digest` | Digest sanitized payload; raw payload не хранится. |
| `schema_version` | Версия normalized envelope. |
| `hook_event_name` | Только MVP event. |
| `attempt_no` | Номер локальной попытки отправки. |
| `next_retry_at` | Локальный backoff checkpoint. |
| `expires_at` | TTL buffer entry; после него применяется failure policy. |
| `event_class` | `audit_critical`, `decision_bridge`, `operational` или `realtime_only`. |
| `envelope_json` | Уже sanitized envelope, размером не больше policy limit. |

Инварианты:

- buffer создаётся после sanitizer и schema validation;
- raw `stdin`, raw prompt, tool payload, stdout/stderr и transcript не записываются;
- переполнение buffer не должно превращаться в молчаливое продолжение рискованного действия;
- sidecar retry использует тот же `event_id`, `payload_digest` и correlation id.

## Политика хранения

| Класс | Что хранится | Retention |
|---|---|---|
| `audit` | Permission/gate/risky decisions, rejected payload facts, source binding failures. | Долгий срок по audit policy платформы. |
| `operational` | Short history для диагностики run/session/slot и восстановления UI. | Короткий срок, например дни, задаётся policy. |
| `realtime` | High-frequency allow events и успешные tool summaries без доменного эффекта. | Минимальный срок, может не попадать в Postgres. |

Полные transcripts, session JSON/JSONL, raw logs, stdout/stderr и вложения должны храниться только во владельцах, если они вообще нужны, и только как object refs с отдельной retention-политикой.

## Индексы и запросы

| Запрос | Индексы |
|---|---|
| Найти событие по `event_id` | `HookEventEnvelope(event_id)` |
| Проверить duplicate/retry | `HookEventEnvelope(event_id, payload_digest)` |
| Лента run/session | `HookOperationalEvent(run_id, session_id, created_at desc)` |
| Pending delivery attempts | `HookDeliveryAttempt(status, next_retry_at)` |
| Ожидающие решения | `HookDecisionBridge(status, expires_at)` |
| Sanitizer metrics | `HookSanitizationReport(reason_code, created_at)` |
| Binding lookup | `HookSourceBinding(source_ref, run_id, session_id, slot_id, status)` |

## Запрещённые модели

- Таблица `Skill`, `SkillManifest`, `SkillInstallation` или `SkillCatalog` внутри `codex-hook-ingress`.
- Таблица `Run`, `AgentSession`, `Slot`, `ProviderArtifact`, `Dialogue`, `PackageManifest`.
- JSONB с raw `tool_input`, `tool_response`, prompt, stdout/stderr или transcript.
- Связи SQL FK в БД соседних сервисов. Межсервисные ссылки хранятся как ids/refs и проверяются через контракты владельцев.

## Миграции

CHI-3/CHI-4 не создают миграции и используют только repository interfaces плюс in-memory stub для проверки service skeleton и route registry. Stub хранит безопасный idempotency record: `event_id`, `payload_digest`, `hook_event_name`, `correlation_id`, `retention_class`, normalized `HookHandlerResult`, safe route delivery diagnostics и время записи. Raw prompt, raw tool input/output, stdout/stderr, transcript, session dump, provider payload, kubeconfig, secret values, `SKILL.md` или package manifest не хранятся.

Перед появлением persistent storage нужно отдельно согласовать:

- нужен ли `codex-hook-ingress` собственный PostgreSQL schema или достаточно общего event log plus short retention store;
- какие таблицы требуются для MVP service skeleton;
- какие retention jobs удаляют operational/realtime данные;
- как audit-critical события попадают в общий audit/event контур.

## Апрув

- request_id: `owner-2026-05-22-codex-hook-ingress-docs`
- Решение: pending
- Комментарий: модель данных описывает границы хранения без создания миграций в docs-first срезе.
