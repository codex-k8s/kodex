---
doc_id: API-CK8S-CODEX-HOOK-INGRESS-0001
type: api-contract
title: codex-hook-ingress - API overview
status: active
owner_role: SA
created_at: 2026-05-22
updated_at: 2026-05-22
related_issues: [698, 753]
related_prs: []
approvals:
  required: ["Owner"]
  status: pending
  request_id: "owner-2026-05-22-codex-hook-ingress-docs"
---

# API overview: codex-hook-ingress

## TL;DR

- Тип API: будущий внутренний command endpoint для нормализованных hook events; конкретный транспорт выбирается отдельным контрактным срезом.
- Аутентификация: source binding для hook emitter/sidecar плюс actor/run/session/slot/scope context.
- Версионирование: версия normalized envelope отдельно от OpenAI hook input и отдельно от будущего transport contract.
- Основные операции: submit hook event, получить hook handler result, проверить health/readiness, отдать bounded operational feed владельцу UI/ops.

Этот документ не является proto, OpenAPI или AsyncAPI. Он фиксирует семантику будущего контракта до реализации.

## Спецификации как source of truth

| Область | Статус |
|---|---|
| Нормализованный hook envelope | Описан в этом API overview и data model; машинная схема создаётся отдельным срезом. |
| Transport contract | Не выбран. Возможные варианты: internal gRPC command или internal HTTP endpoint за сервисной mesh-границей. |
| OpenAPI | Не создаётся в docs-first срезе. Внешняя пользовательская HTTP-поверхность не планируется. |
| AsyncAPI | Не создаётся в docs-first срезе. Downstream events проектируются после выбора event transport. |
| MCP | Не применяется. MCP discovery and calls обслуживает `platform-mcp-server`. |

## Операции

| Operation | Method/Topic | Auth | Idempotency | Notes |
|---|---|---|---|---|
| `SubmitHookEvent` | internal command, transport TBD | Source binding + run/session/slot/scope | `event_id` + `payload_digest` | Принимает normalized envelope и возвращает hook handler result для Codex command hook. |
| `GetHookIngressStatus` | internal read | Service/operator auth | нет | Readiness, версия схемы, включённые events, лимиты и dependency status без секретов. |
| `ListRecentHookOperationalEvents` | internal read | Ops/UI route auth | cursor | Короткая безопасная лента по run/session/slot. Может быть реализована через operations/interaction контур. |
| `AckHookDelivery` | internal callback, optional | Downstream service auth | `event_id` + route | Подтверждает доставку, если выбран asynchronous route. |

`SubmitHookEvent` является единственной обязательной MVP-операцией. Остальные операции могут быть реализованы соседними контурами или отложены, если будут лишними для MVP.

## DTO: SubmitHookEventRequest

| Field | Type | Required | Notes |
|---|---|---:|---|
| `event_id` | string/uuid | да | Идемпотентность. |
| `schema_version` | string | да | Версия normalized envelope. |
| `hook_event_name` | enum | да | Только MVP set. |
| `event_time` | timestamp | да | Время runtime. |
| `source_context` | object | да | Actor/source/org/project/repository refs без секретов. |
| `run_context` | object | да | `run_id`, `session_id`, `slot_id`, optional `turn_id`, `role_ref`, `stage_ref`. |
| `tool_context` | object | нет | Для tool-scoped events: `tool_name`, `tool_category`, `tool_use_id`, command hash, path category. |
| `capability_context` | object | нет | Только refs/digests selected by `agent-manager` and materialized by `runtime-manager`. |
| `safe_payload` | object | да | Sanitized payload, bounded previews, exit status, artifact signals. |
| `payload_digest` | string | да | Digest normalized significant data. |
| `correlation_id` | string | да | Сквозная корреляция. |

## DTO: HookHandlerResult

| Field | Type | Required | Notes |
|---|---|---:|---|
| `result` | enum | да | Нормализованный платформенный результат: `continue`, `allow`, `deny`, `no_decision`, `retry`, `fail_closed`, `ignored`. |
| `hook_event_name` | enum | да | Повторяет событие. |
| `system_message` | string | нет | Safe text для Codex UI/event stream, если policy разрешает. |
| `additional_context` | string | нет | Safe model-visible context, если разрешён владельцем. |
| `decision_reason` | string | нет | Sanitized reason. |
| `stop_reason` | string | нет | Только для events, где Codex поддерживает stop/continue semantics. |
| `updated_input_ref` | object | нет | По умолчанию запрещён. В MVP не использовать без отдельного решения. |
| `owner_decision_ref` | string | нет | Ссылка на gate/decision у `agent-manager`. |
| `correlation_id` | string | да | Для связи с request. |

`HookHandlerResult` — внутренняя нормализованная модель, а не дословный JSON stdout Codex hook. Emitter/sidecar обязан маппить её в поддерживаемый Codex output по конкретному event:

- для `PermissionRequest` допустимы `allow`, `deny` или отсутствие hook-specific decision (`no_decision`), после которого Codex продолжает штатный approval flow только если это разрешено policy владельца;
- для `PreToolUse` нельзя возвращать `permissionDecision: "ask"`; поддерживаются только безопасный `deny`, дополнительный контекст или разрешённое policy изменение input там, где это поддержано Codex runtime;
- `fail_closed` означает безопасный отказ или ошибку hook handler для рискованных действий, а не молчаливое продолжение.

## Event-specific contract

### `SessionStart`

Input:

- `source`: `startup`, `resume` или `clear`;
- `model`;
- `cwd_ref` или workspace ref после нормализации;
- `capability_context_ref`, если run стартует с выбранными skills.

Routes:

- `agent-manager`: связать Codex session с `AgentSession` and `Run`;
- `runtime-manager`: подтвердить slot/session/workspace binding.

### `UserPromptSubmit`

Input:

- prompt hash;
- safe prompt class or summary;
- policy pre-check result;
- turn id.

Routes:

- `agent-manager`: факт нового turn и policy decision;
- `interaction-hub`: только если prompt является пользовательской перепиской с отдельной retention-политикой.

Ingress не хранит полный prompt.

### `PreToolUse`

Input:

- `tool_name`, `tool_category`, `tool_use_id`;
- command hash and bounded sanitized preview для shell/patch;
- MCP tool name только как имя, не как MCP call;
- `capability_context_ref` or `skill_ref`, если tool вызван из skill workflow.

Routes:

- `agent-manager`: gate/risk decision или realtime event;
- `runtime-manager`: workspace/runtime diagnostics;
- operations/realtime feed: safe preview.

`PreToolUse` не является полной enforcement boundary. Он даёт ранний сигнал и может блокировать поддерживаемые tool calls только в пределах возможностей Codex hook runtime и policy владельца.

### `PermissionRequest`

Input:

- tool category/name;
- sanitized human reason;
- requested permission class;
- timeout budget;
- risk class.

Routes:

- `agent-manager`: создать или найти gate/decision;
- `interaction-hub`: доставить owner feedback request, если требуется человек.

Output:

- allow/deny/no_decision/fail_closed с sanitized reason.

### `PostToolUse`

Input:

- tool category/name;
- exit status;
- bounded error;
- output digest;
- provider artifact signal, если найден;
- rate-limit hint, если безопасно извлечён.

Routes:

- `provider-hub`: hot reconciliation hint or provider artifact signal;
- `runtime-manager`: runtime diagnostic summary;
- `agent-manager`: run state or acceptance signal;
- operations/realtime feed.

Ingress не пытается undo side effects.

### `Stop`

Input:

- latest assistant message hash или safe summary;
- pending action refs;
- provider signal refs;
- checkpoint ref, если его создал владелец.

Routes:

- `agent-manager`: turn checkpoint, pending gates, run state;
- `runtime-manager`: session/workspace checkpoint refs;
- `provider-hub`: provider hot cursor hints;
- `interaction-hub`: notification/feedback intents.

## Модель ошибок

| Error code | Когда возвращается |
|---|---|
| `hook.unsupported_event` | Event name вне MVP-набора. |
| `hook.invalid_context` | Нет actor/source/run/session/slot/scope context. |
| `hook.invalid_binding` | Source binding не найден, истёк или не совпадает с run/session/slot. |
| `hook.payload_too_large` | Envelope или preview превышает лимит. |
| `hook.payload_rejected` | Forbidden field, binary data, raw transcript/session dump или secret-like content. |
| `hook.duplicate_conflict` | Повторный `event_id` пришёл с другим digest. |
| `hook.rate_limited` | Превышен лимит source/run/event class. |
| `hook.owner_unavailable` | Downstream-владелец недоступен. |
| `hook.decision_timeout` | Для permission/pre-tool bridge не получено решение до timeout. |
| `hook.route_rejected` | Сервис-владелец отклонил safe event по доменным правилам. |

Ошибки должны быть audit-safe: без secret values, raw command, raw stdout/stderr, full prompt и provider payload.

## Rate limits и backpressure

| Лимит | Начальное правило |
|---|---|
| Envelope size | 64 KiB. |
| Preview per text field | 4 KiB. |
| Bounded error | 8 KiB. |
| High-frequency realtime events | Лимит по source/run/event class, с деградацией до sampling. |
| Audit-critical events | Не дропать молча; при перегрузке возвращать fail-closed или retryable error. |
| Decision bridge timeout | Определяется `agent-manager` policy; risky requests fail-closed. |

Конкретные значения должны стать typed platform settings или transport config с audit trail.

## Совместимость

- Новые поля envelope добавляются как optional до новой major-версии схемы.
- Новые hook events требуют отдельного решения владельца и обновления PRD/design/API.
- Удаление поля проходит через deprecation period в schema version.
- Изменение route semantics требует синхронизации с владельцем downstream-сервиса.
- `capability_context` остаётся ссылочным объектом; перенос skill manifest в hook API считается breaking boundary violation.

## Апрув

- request_id: `owner-2026-05-22-codex-hook-ingress-docs`
- Решение: pending
- Комментарий: API overview фиксирует будущую семантику без proto, OpenAPI, AsyncAPI и кода.
