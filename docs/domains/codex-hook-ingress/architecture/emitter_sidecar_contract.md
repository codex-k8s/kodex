---
doc_id: ARC-CK8S-CODEX-HOOK-EMITTER-0001
type: api-contract
title: codex-hook-ingress - контракт hook emitter и local sidecar
status: active
owner_role: SA
created_at: 2026-05-25
updated_at: 2026-05-25
related_issues: [698, 753, 778, 786, 322]
related_prs: []
related_docsets:
  - docs/domains/codex-hook-ingress/product/requirements.md
  - docs/domains/codex-hook-ingress/architecture/design.md
  - docs/domains/codex-hook-ingress/architecture/api_contract.md
  - specs/jsonschema/codex-hook-ingress.v1/hook-emitter-config.v1.schema.json
approvals:
  required: ["Owner"]
  status: pending
  request_id: "owner-2026-05-25-codex-hook-emitter-sidecar"
---

# Контракт hook emitter и local sidecar

## TL;DR

- Hook emitter запускается как Codex command hook внутри slot и получает один JSON object на `stdin`.
- Local sidecar является опциональным локальным процессом рядом с Codex runtime; он может держать bounded retry buffer и отправлять уже очищенные envelope.
- Единственный платформенный получатель CHI-2 — `codex-hook-ingress`; `integration-gateway` остаётся входом для внешних webhook/callback.
- Физический транспорт `SubmitHookEvent` не выбран в CHI-2. Контракт фиксирует логическую операцию, auth, idempotency, ordering, retry, backpressure и failure policy.
- Emitter/sidecar не читает `transcript_path`, не хранит raw prompt/tool payload, не буферизует секреты и не принимает бизнес-решения.

## Runtime-роль

| Компонент | Где запускается | Ответственность |
|---|---|---|
| Hook emitter | Как command hook Codex в рабочем пространстве slot. | Принять JSON на `stdin`, проверить event allowlist, нормализовать минимальный input, применить sanitizer, создать envelope, получить `HookHandlerResult` и смэппить его в поддерживаемый Codex output. |
| Local sidecar | Как локальный процесс рядом с Codex runtime в том же slot/runtime boundary. | Принять очищенный event от thin emitter, держать bounded retry buffer, повторять отправку, контролировать backpressure и не хранить raw payload. |
| `runtime-manager` | Владелец slot/workspace/runtime job. | Подготовить размещение emitter/sidecar, endpoint ref, workload identity/source token и materialized capability refs. |
| `codex-hook-ingress` | Внутренний сервис платформы. | Принять normalized envelope, проверить source binding, idempotency, sanitizer report и маршрутизацию safe parts владельцам. |

Emitter может быть самостоятельным hook command без sidecar, если он успевает выполнить sanitizer и отправку в пределах hook timeout. Sidecar нужен, когда требуется устойчивый retry buffer, shared auth material и единая отправка для нескольких hook commands. Оба варианта обязаны производить один и тот же normalized envelope.

## Источник hook input

Codex command hook передаёт hook input как JSON object на `stdin`. CHI-2 использует это как стабильную границу чтения и не опирается на формат transcript/session-файлов.

Разрешённые реальные Codex hook events для платформы:

| Event | Scope | Режим доставки |
|---|---|---|
| `SessionStart` | thread/session | Асинхронная доставка с bounded flush. |
| `UserPromptSubmit` | turn | Асинхронная доставка после sanitizer; prompt raw не хранится в ingress. |
| `PreToolUse` | turn/tool | Синхронная доставка только если policy требует решения; иначе operational/realtime event. |
| `PermissionRequest` | turn/tool approval | Синхронный decision bridge с timeout и fail-closed/no_decision по policy. |
| `PostToolUse` | turn/tool | Асинхронная доставка; bounded error и artifact signals без stdout/stderr. |
| `Stop` | turn/session | Bounded flush перед завершением хода; при недоступности платформы применяется failure policy. |

`PreCompact` и `PostCompact` не являются hook events платформенного MVP. Будущие compact checkpoints оформляются как внутренние события `agent-manager`/`runtime-manager`, например `SessionCompactCheckpointRequested` и `SessionCompactCheckpointCompleted`, и не попадают в `supported_hook_events`.

## Нормализация и sanitizer pipeline

1. Принять JSON object из `stdin` и отклонить не-JSON или payload больше локального pre-parse лимита.
2. Проверить `hook_event_name` по MVP allowlist.
3. Добавить platform context: source/run/session/slot/scope refs, `correlation_id`, `event_id`, `emitter_version`.
4. Удалить или заменить forbidden fields: raw `tool_input`, raw `tool_response`, `stdout`, `stderr`, `transcript_path`, `session_dump`, `prompt.raw`, env, headers, kubeconfig, provider payload, secret-like values, `SKILL.md` и package manifest.
5. Сформировать bounded previews, digests, refs и `sanitizer_report` по `sanitizer-contract.v1`.
6. Проверить envelope по `normalized-hook-envelope.v1`.
7. Передать envelope в `SubmitHookEvent` или положить только sanitized envelope в local buffer.

Sanitizer выполняется до записи в buffer и до сетевой отправки. Raw payload не должен появляться в логах, метриках, retry queue, crash dump или локальном spool.

## Контракт отправки

| Поле | Решение CHI-2 |
|---|---|
| Receiver | Только `codex-hook-ingress`. |
| Logical operation | `SubmitHookEvent`. |
| Payload | `normalized-hook-envelope.v1`. |
| Response | `HookHandlerResult`, который emitter маппит в supported Codex hook output. |
| Physical protocol | Не выбран: будущий internal gRPC command или internal HTTP endpoint. |
| Endpoint | Не raw URL в документации; `runtime-manager` выдаёт endpoint ref для slot/runtime. |
| Auth | Workload identity, short-lived source token или mTLS service identity; secret material не хранится в config. |
| Idempotency | `event_id + payload_digest`; повтор с тем же digest возвращает прежний результат или ack, повтор с другим digest отклоняется. |
| Ordering | Гарантия best-effort per `run_id/session_id` через local monotonic sequence, `event_time` и `received_time`; downstream владельцы остаются идемпотентными. |
| Size limits | Envelope до 64 KiB, text preview до 4 KiB, bounded error до 8 KiB. |
| Redaction | По `sanitizer-contract.v1`; секреты и raw payload запрещены до buffer/send. |

OpenAPI и AsyncAPI не создаются в CHI-2, потому что они зафиксировали бы физический транспорт раньше решения владельца. Machine-readable часть CHI-2 — `hook-emitter-config.v1.schema.json`, которая описывает runtime config и delivery policy без выбора gRPC/HTTP.

## Retry, buffer и backpressure

| Ситуация | Поведение |
|---|---|
| Платформа временно недоступна | Sidecar сохраняет только sanitized envelope в bounded buffer и повторяет отправку с exponential backoff+jitter до TTL. |
| Buffer переполнен | Realtime-only events отбрасываются с metric; audit-critical/decision events получают fail-closed или controlled no_decision по policy. |
| Частичная доставка downstream routes | `codex-hook-ingress` или sidecar повторяет недоставленные routes с тем же `event_id`; получатели обязаны быть идемпотентными. |
| Sanitizer отказал payload | Raw event не буферизуется, не отправляется и не логируется; сохраняется только audit-safe причина отказа. |
| Истёк timeout `PermissionRequest` | Рискованные действия завершаются fail-closed; `no_decision` допускается только если policy разрешает штатный Codex approval flow. |
| Истёк timeout `PreToolUse` с policy decision | Нельзя маппить ожидание в `permissionDecision: "ask"`; действие получает deny/fail-closed или safe continue только по policy. |

Buffer не является источником истины. Он нужен только для короткой устойчивости доставки между slot runtime и ingress. Если используется локальный spool, он должен быть зашифрован и очищаться при завершении slot/session или истечении TTL.

## Mapping результата в Codex

`HookHandlerResult` остаётся платформенной моделью, а не дословным stdout. Emitter обязан учитывать поддержку конкретного Codex event:

- `PermissionRequest`: `allow`, `deny` или отсутствие hook-specific decision (`no_decision`).
- `PreToolUse`: safe `deny`, разрешённый additional context или policy-controlled `allow` с `updatedInput`, если это поддержано; `permissionDecision: "ask"` запрещён.
- `PostToolUse`: safe feedback/additional context; ingress не откатывает side effects.
- `SessionStart`, `UserPromptSubmit`, `Stop`: только supported output fields и bounded safe text.

Любой unsupported output должен превращаться в bounded safe error, а не в raw stderr с payload.

## Machine-readable config

Стартовый runtime contract описан в:

- `specs/jsonschema/codex-hook-ingress.v1/hook-emitter-config.v1.schema.json`;
- `specs/jsonschema/codex-hook-ingress.v1/hook-emitter-config.defaults.json`.

Схема фиксирует:

- `supported_hook_events` ровно из MVP-набора;
- compact checkpoints как `internal_session_events`, а не Codex hooks;
- logical receiver `codex-hook-ingress` и operation `SubmitHookEvent`;
- отсутствие выбранного physical transport;
- auth без секретов в config;
- retry, buffer, backpressure и failure policy;
- запрет raw payload storage.

## Открытое решение

| Вопрос | Варианты | Влияние |
|---|---|---|
| Физический транспорт `SubmitHookEvent` | Internal gRPC command или internal HTTP endpoint внутри service mesh. | Блокирует OpenAPI/gRPC/proto контракт и сервисный каркас CHI-3, но не блокирует CHI-2 runtime contract. |

До принятия решения реализация тяжёлого runtime, network client, proto, OpenAPI и AsyncAPI не начинается.

## Апрув

- request_id: `owner-2026-05-25-codex-hook-emitter-sidecar`
- Решение: pending
- Комментарий: CHI-2 фиксирует логический runtime contract emitter/sidecar без выбора физического transport contract.
