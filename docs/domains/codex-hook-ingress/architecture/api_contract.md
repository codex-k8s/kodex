---
doc_id: API-CK8S-CODEX-HOOK-INGRESS-0001
type: api-contract
title: codex-hook-ingress - API overview
status: active
owner_role: SA
created_at: 2026-05-22
updated_at: 2026-05-26
related_issues: [698, 753, 778, 786, 793, 808, 823, 836, 322, 834]
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
| Нормализованный hook envelope | Machine-readable source of truth: `specs/jsonschema/codex-hook-ingress.v1/normalized-hook-envelope.v1.schema.json`. |
| Sanitizer contract | Machine-readable source of truth: `specs/jsonschema/codex-hook-ingress.v1/sanitizer-contract.v1.schema.json` и стартовый экземпляр `sanitizer-contract.defaults.json`. |
| Hook emitter/local sidecar runtime config | Machine-readable source of truth: `specs/jsonschema/codex-hook-ingress.v1/hook-emitter-config.v1.schema.json` и стартовый экземпляр `hook-emitter-config.defaults.json`. |
| Safe examples | `specs/jsonschema/codex-hook-ingress.v1/examples/*.safe.json`; примеры не содержат raw payload, секреты, stdout/stderr или session dumps. |
| Transport contract | Не выбран. Возможные варианты: internal gRPC command или internal HTTP endpoint за сервисной mesh-границей. |
| OpenAPI | Не создаётся в CHI-1/CHI-2. Внешняя пользовательская HTTP-поверхность не планируется, а physical transport `SubmitHookEvent` не выбран. |
| AsyncAPI | Не создаётся в CHI-1/CHI-2. Downstream events проектируются после выбора event transport. |
| MCP | Не применяется. MCP discovery and calls обслуживает `platform-mcp-server`. |

Проверка CHI-1/CHI-2 выполняется JSON Schema validation для safe examples, sanitizer defaults и hook emitter defaults. Генерация Go-кода не выполняется, потому что JSON Schema описывает pre-transport payload/runtime policy; кодовые DTO, proto/gRPC или HTTP transport создаются отдельным срезом после выбора транспорта.

## Операции

| Operation | Method/Topic | Auth | Idempotency | Notes |
|---|---|---|---|---|
| `SubmitHookEvent` | internal command, transport TBD | Source binding + run/session/slot/scope | `event_id` + `payload_digest` | Принимает normalized envelope и возвращает hook handler result для Codex command hook. |
| `GetHookIngressStatus` | internal read | Service/operator auth | нет | Readiness, версия схемы, включённые events, лимиты и dependency status без секретов. |
| `ListRecentHookOperationalEvents` | internal read | Ops/UI route auth | cursor | Короткая безопасная лента по run/session/slot. В сервисном MVP существует только in-process read boundary поверх bounded in-memory feed; физический endpoint не выбран. |
| `AckHookDelivery` | internal callback, optional | Downstream service auth | `event_id` + route | Подтверждает доставку, если выбран asynchronous route. |

`SubmitHookEvent` является единственной обязательной MVP-операцией. Остальные операции могут быть реализованы соседними контурами или отложены, если будут лишними для MVP.

Logical response CHI-4 также содержит route delivery diagnostics. Unsupported, disabled или failed route не считается успешной доставкой. Diagnostic text должен быть safe: без raw downstream error, prompt, tool input/output, stdout/stderr, provider payload, kubeconfig и secret values.

Persistent история tool/activity не является операцией `codex-hook-ingress`. Отдельный CHI-срез маршрутизирует sanitized `PreToolUse`/`PostToolUse` в `agent-manager.RecordAgentActivity`; `codex-hook-ingress` остаётся sanitizer/router/realtime ops feed и не хранит долгую историю tool calls.

## Состояние реализации CHI-3/CHI-4/CHI-5/CHI-6a

Кодовый каркас `services/internal/codex-hook-ingress` реализует `SubmitHookEvent` только как in-process logical boundary в `internal/transport/command`. Он нужен для проверки доменного use-case, idempotency и sanitizer boundary без фиксации physical transport.

CHI-4 добавляет route registry и owner ports/stubs для dispatch безопасных частей события к `agent-manager`, `runtime-manager`, `provider-hub`, `governance-manager`, `interaction-hub`, operations/realtime placeholder и audit placeholder. Registry строит canonical route plan по `hook_event_name`; `downstream_routes` из envelope сверяются с этой матрицей и не являются источником истины для dispatch. Registry проецирует только canonical `safe_parts`, возвращает safe delivery diagnostics и не вызывает business command у соседнего домена.

Process config CHI-4 добавляет `KODEX_CODEX_HOOK_INGRESS_DISABLED_ROUTES` для отключения отдельных owner routes и `KODEX_CODEX_HOOK_INGRESS_ROUTE_FAILURE_POLICY` со значениями `diagnostic` или `fail_closed`. В режиме `diagnostic` неуспешные routes отражаются только в diagnostics; в режиме `fail_closed` handler result становится безопасным `fail_closed`.

Idempotency record имеет состояние завершённости delivery. Повтор после уже завершённого delivery возвращает cached diagnostics; повтор после incomplete delivery не считается успешным cached replay и пытается дозавершить canonical dispatch.

CHI-5 добавляет owner decision bridge для `PermissionRequest` и policy-controlled `PreToolUse`. Bridge строит только safe request context, вызывает owner ports/stubs `governance-manager`, `agent-manager` и `interaction-hub`, возвращает explicit handler state и пишет bounded diagnostics. Ingress не хранит persistent историю tool/activity; долгосрочная timeline принадлежит `agent-manager`.

Process config CHI-5 добавляет `KODEX_CODEX_HOOK_INGRESS_DECISION_BRIDGE_TIMEOUT`, `KODEX_CODEX_HOOK_INGRESS_PERMISSION_DECISION_FAILURE_POLICY`, `KODEX_CODEX_HOOK_INGRESS_PRE_TOOL_USE_DECISION_FAILURE_POLICY` и `KODEX_CODEX_HOOK_INGRESS_PRE_TOOL_USE_DECISION_RISK_CLASSES`. Failure policy поддерживает только `fail_closed`, `no_decision`, `timeout` и `retryable_error`; неподдерживаемый Codex output `ask` не используется.

CHI-6a добавляет bounded in-memory ops/realtime feed, безопасный snapshot diagnostics, sanitizer/route counters, payload/latency buckets, fixed-window logical rate limit и backpressure перед downstream dispatch. Ops entry содержит только safe summary, event kind, route result, owner target, digest, size bucket, status, reject reason и timestamps. Перегрузка возвращает `hook.rate_limited` или `hook.backpressure` и не считается успешной доставкой.

В CHI-3/CHI-4/CHI-5/CHI-6a не создаются proto, OpenAPI, AsyncAPI, HTTP/gRPC handler для `SubmitHookEvent` и network client emitter/sidecar. Служебный HTTP-процесс отдаёт только `/health/livez`, `/health/readyz` и `/metrics`.

## Логический контракт hook emitter/local sidecar

CHI-2 фиксирует поведение вызывающей стороны `SubmitHookEvent`, но не выбирает physical transport.

| Область | Контракт |
|---|---|
| Source | Codex command hook передаёт один JSON object на `stdin`; emitter не читает `transcript_path` и не зависит от формата session transcript. |
| Receiver | Только `codex-hook-ingress`; `integration-gateway` не принимает slot-local hook events. |
| Endpoint | `runtime-manager` выдаёт endpoint ref внутри slot/runtime boundary; raw URL и секреты не фиксируются в документации. |
| Protocol | `transport_tbd_internal_command`: future gRPC command или internal HTTP endpoint. |
| Auth | Source binding плюс workload identity, short-lived source token или mTLS service identity. Secret material запрещён в config. |
| Payload | Только `normalized-hook-envelope.v1` после sanitizer. |
| Buffer | Только normalized/sanitized envelope, bounded by count/bytes/TTL; raw payload не буферизуется. |
| Retry | Повтор с тем же `event_id`, `payload_digest` и correlation id; non-retryable sanitizer/binding errors не повторяются. |
| Backpressure | Realtime-only events можно отбрасывать с metric; audit-critical и decision events fail-closed или retry until TTL по policy. |
| Response mapping | Emitter маппит `HookHandlerResult` в поддерживаемый Codex stdout/stderr/exit code для конкретного event. |

Machine-readable конфигурация: `specs/jsonschema/codex-hook-ingress.v1/hook-emitter-config.v1.schema.json`.

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
| `sanitizer_report` | object | да | Audit-safe факт очистки: результат, применённые правила, счётчики redaction/truncation без значений. |
| `downstream_routes` | array | да | Список владельцев и safe parts, которые может получить каждый downstream route. |
| `correlation_id` | string | да | Сквозная корреляция. |
| `retention_class` | enum | да | `audit`, `operational` или `realtime`; не заменяет retention владельцев. |

## DTO: HookHandlerResult

| Field | Type | Required | Notes |
|---|---|---:|---|
| `result` | enum | да | Нормализованный платформенный результат: `continue`, `allow`, `deny`, `no_decision`, `timeout`, `retry`, `retryable_error`, `fail_closed`, `ignored`. |
| `hook_event_name` | enum | да | Повторяет событие. |
| `system_message` | string | нет | Safe text для Codex UI/event stream, если policy разрешает. |
| `additional_context` | string | нет | Safe model-visible context, если разрешён владельцем. |
| `decision_reason` | string | нет | Sanitized reason. |
| `stop_reason` | string | нет | Только для events, где Codex поддерживает stop/continue semantics. |
| `updated_input_ref` | object | нет | По умолчанию запрещён. В MVP не использовать без отдельного решения. |
| `owner_decision_ref` | string | нет | Ссылка на gate/decision у `governance-manager` или flow-wait ref у `agent-manager`. |
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

- `agent-manager`: факт нового turn и lifecycle context;
- `interaction-hub`: только если prompt является пользовательской перепиской с отдельной retention-политикой.

Ingress не хранит полный prompt.

### `PreToolUse`

Input:

- `tool_name`, `tool_category`, `tool_use_id`;
- command hash and bounded sanitized preview для shell/patch;
- MCP tool name только как имя, не как MCP call;
- `capability_context_ref` or `skill_ref`, если tool вызван из skill workflow.

Routes:

- `agent-manager`: safe activity timeline entry, realtime event или flow-wait ref;
- `governance-manager`: risk context или policy-controlled decision ref;
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

- `governance-manager`: создать или найти gate/decision;
- `agent-manager`: зафиксировать ожидание flow, если действие связано с агентным переходом;
- `interaction-hub`: доставить owner feedback или Human gate request, если требуется человек.

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
- `agent-manager`: safe activity timeline entry, run state or acceptance signal;
- operations/realtime feed.

Ingress не пытается undo side effects.

### `Stop`

Input:

- latest assistant message hash или safe summary;
- pending action refs;
- provider signal refs;
- checkpoint ref, если его создал владелец.

Routes:

- `agent-manager`: turn checkpoint, pending gate refs, flow waiting refs, run state;
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
| `hook.backpressure` | Bounded ops/feed admission не может безопасно принять событие перед dispatch. |
| `hook.owner_unavailable` | Downstream-владелец недоступен. |
| `hook.decision_timeout` | Для permission/pre-tool bridge не получено решение от governance/interaction контуров до timeout. |
| `hook.route_rejected` | Сервис-владелец отклонил safe event по доменным правилам. |

Ошибки должны быть audit-safe: без secret values, raw command, raw stdout/stderr, full prompt и provider payload.

## Rate limits и backpressure

| Лимит | Начальное правило |
|---|---|
| Envelope size | 64 KiB. |
| Preview per text field | 4 KiB. |
| Bounded error | 8 KiB. |
| Ops feed capacity | Bounded in-memory feed, стартовое значение `1024` entries через `KODEX_CODEX_HOOK_INGRESS_OPS_FEED_CAPACITY`. |
| Ops feed retention | Стартовое значение `15m` через `KODEX_CODEX_HOOK_INGRESS_OPS_FEED_RETENTION`; записи удаляются из памяти без persistent storage. |
| High-frequency realtime events | Fixed-window limit по source/run/event class, стартово `300` событий за `1m` через `KODEX_CODEX_HOOK_INGRESS_RATE_LIMIT_BURST` и `KODEX_CODEX_HOOK_INGRESS_RATE_LIMIT_WINDOW`. |
| Audit-critical events | Не дропать молча; при перегрузке возвращать fail-closed или retryable error. |
| Decision bridge timeout | Стартовое значение `30s` через `KODEX_CODEX_HOOK_INGRESS_DECISION_BRIDGE_TIMEOUT`; меньший `timeout_budget_ms` из safe payload может ужать ожидание. |
| Permission decision failure policy | Стартовое значение `fail_closed` через `KODEX_CODEX_HOOK_INGRESS_PERMISSION_DECISION_FAILURE_POLICY`. |
| PreToolUse decision failure policy | Стартовое значение `no_decision` через `KODEX_CODEX_HOOK_INGRESS_PRE_TOOL_USE_DECISION_FAILURE_POLICY`. |
| PreToolUse decision risk classes | Стартовое значение `medium,high,unknown` через `KODEX_CODEX_HOOK_INGRESS_PRE_TOOL_USE_DECISION_RISK_CLASSES`; `low` не блокируется без отдельной policy. |

Текущие значения являются service config для MVP. Если они должны меняться на лету, следующий срез переводит их в typed platform settings с audit trail.

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
