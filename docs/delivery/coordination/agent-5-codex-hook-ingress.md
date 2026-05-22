# Агент #5 — входной контур Codex hooks

## Зона ответственности

Агент #5 ведёт домен `codex-hook-ingress`.

`codex-hook-ingress` отвечает за:

- приём нормализованных Codex hook events от hook emitter или локального sidecar;
- machine-readable схемы normalized hook envelope и sanitizer contract;
- проверку actor/source/run/session/slot binding на входной границе;
- очистку входа, размерные лимиты, redaction, безопасные preview, digest и refs;
- маршрутизацию безопасных частей hook events к владельцам: `agent-manager`, `runtime-manager`, `provider-hub`, `governance-manager`, `interaction-hub` и realtime/operations контуру;
- отделение Codex hooks от MCP tools, MCP transport и business commands;
- сохранение Codex skills только как `capability_context_ref`, `skill_ref` и digest без каталога skills или manifest-хранилища в ingress.

`codex-hook-ingress` не владеет:

- MCP tools, `tools/list`, `tools/call` и MCP transport — это зона `platform-mcp-server`;
- `Run`, session, flow, stage, role и состояние ожидания flow — это зона `agent-manager`;
- risk/gate decision state — это зона `governance-manager`;
- slot, workspace, materialization и runtime job — это зона `runtime-manager`;
- provider-native artifacts и write pipeline — это зона `provider-hub`;
- delivery, callbacks, диалоги и уведомления — это зона `interaction-hub`;
- package source/version/install/manifest и каталог skills — это зона `package-hub`.

## Что уже сделано

| Срез | Issue | Статус | Результат |
|---|---:|---|---|
| CHI-0 | #698 | готово | Доменная документация `codex-hook-ingress`: требования, дизайн, модель состояния, API overview, delivery-план, карта Issue и связь со сквозной архитектурой hooks/skills. |
| CHI-1 | #778 | готово | Machine-readable JSON Schema для normalized hook envelope и sanitizer contract, safe examples, индексы спецификаций и обновлённая трассируемость. |

## Текущий бэклог агента #5

| Срез | Что осталось |
|---|---|
| CHI-2 | Hook emitter или local sidecar: чтение Codex hook JSON, redaction, size limits, retry buffer и безопасная отправка envelope. |
| CHI-3 | Сервисный каркас `codex-hook-ingress`: process, config, health/readiness, metrics, source verifier, sanitizer и idempotency. |
| CHI-4 | Routes к `agent-manager`, `runtime-manager`, `provider-hub`, `governance-manager`, `interaction-hub` и operations/realtime контуру для safe event parts. |
| CHI-5 | `PermissionRequest` и policy-controlled `PreToolUse` bridge через `governance-manager`, ожидание flow у `agent-manager` и delivery через `interaction-hub`. |
| CHI-6 | Realtime/ops feed, retention, sanitizer metrics, rate limits, backpressure и operator diagnostics. |
| CHI-7 | Capability context refs для Codex skills без skill catalog в ingress. |
| CHI-8 | Deploy-контур: Dockerfile, manifests, migration job только если нужна служебная БД, smoke, runbook и monitoring. |

## Синхронизация с соседними доменами

| Домен или сервис | Что согласовывать |
|---|---|
| `platform-mcp-server` | Hooks не являются MCP tools; MCP видит только tool calls, а hook ingress может фиксировать MCP tool name как безопасное имя в `tool_context`. |
| `agent-manager` | Run/session binding, flow waiting refs, lifecycle events, stop checkpoint и capability context selection. |
| `governance-manager` | Risk assessment, gate request/decision refs и fail-closed policy для рискованных `PermissionRequest` и `PreToolUse`. |
| `runtime-manager` | Slot/session binding, workspace refs, materialized capability refs, local emitter/sidecar placement и runtime diagnostics. |
| `provider-hub` | Provider artifact signals, rate-limit hints и hot reconciliation hints без provider payload и stdout `gh`. |
| `interaction-hub` | Delivery request refs, owner feedback, Human gate prompt и callback refs без хранения decision state в interaction. |
| `package-hub` | Package-backed skill source/version/install/manifest refs; ingress не хранит manifest или `SKILL.md`. |
| `access-manager` | Source binding, actor policy, tool/capability use policy и audit-safe authorization facts. |

## Блокировки и правила работы

- Не добавлять `PreCompact` и `PostCompact` в Codex hook set; compact checkpoints проектируются только как внутренние события `agent-manager`/`runtime-manager`.
- Не переносить MCP `tools/list` или `tools/call` в hook ingress.
- Не создавать skill catalog, package manifest store или materialization state внутри `codex-hook-ingress`.
- Не хранить raw `tool_input`, raw `tool_response`, prompt, stdout/stderr, transcript, session dump, kubeconfig, provider payload или secret values.
- Следующий кодовый срез должен опираться на machine-readable схемы CHI-1 и не начинать транспортный контракт без отдельного решения по gRPC/HTTP.
