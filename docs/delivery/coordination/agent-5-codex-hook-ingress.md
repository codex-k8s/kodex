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
- сохранение Codex skills только как `capability_context_ref`, `skill_ref`, source/package/version refs и digest без каталога skills, manifest-хранилища или materialization state в ingress.

`codex-hook-ingress` не владеет:

- MCP tools, `tools/list`, `tools/call` и MCP transport — это зона `platform-mcp-server`;
- `Run`, session, flow, stage, role, persistent activity timeline и состояние ожидания flow — это зона `agent-manager`;
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
| CHI-2 | #786 | готово | Контракт hook emitter/local sidecar, runtime config JSON Schema, logical `SubmitHookEvent`, sanitizer до buffer/send, auth, idempotency, ordering, retry, bounded buffer, backpressure и failure policy без выбора physical transport. |
| CHI-3 | #793 | готово | Сервисный каркас `services/internal/codex-hook-ingress`: process, config, graceful shutdown, health/readiness/metrics, in-process logical `SubmitHookEvent`, source binding placeholder, schema validation hook, sanitizer boundary, idempotency repository stub без raw payload storage. |
| CHI-4 | #808 | готово | Route registry и dispatch безопасных частей hook events через owner ports/stubs к `agent-manager`, `runtime-manager`, `provider-hub`, `governance-manager`, `interaction-hub` и operations/realtime placeholder; unsupported/disabled/downstream-failed routes дают safe diagnostics и не считаются успешной доставкой. |
| CHI-4b | #844 | готово | Sanitized `PreToolUse`/`PostToolUse` маршрутизируются в typed owner port `agent-manager.RecordAgentActivity`: формируется safe activity record с session/run/slot/turn refs, tool metadata, status, timestamps, digest, bounded error, capability refs и correlation/idempotency trace; persistent timeline остаётся у `agent-manager`. |
| CHI-5 | #836 | готово | `PermissionRequest` bridge и policy-controlled `PreToolUse`: safe request context, owner decision ports/stubs для `governance-manager`, `agent-manager`, `interaction-hub`, explicit handler states, timeout/fail-closed policy и idempotent replay без persistent истории tool/activity в ingress. |
| CHI-6a | #823 | готово | Bounded in-memory ops/realtime feed, TTL/capacity retention, sanitizer metrics, route diagnostics, fixed-window rate limits, safe backpressure и operator diagnostics поверх CHI-4 registry без служебной БД. |
| CHI-7 | #854 | готово | Capability context refs для Codex skills проходят через normalized envelope, sanitizer boundary, route registry и `agent-manager.RecordAgentActivity` как refs/digests без skill catalog, manifest payload, package installation state или workspace paths в ingress. |
| CHI-8 | #868 | готово | Deploy-контур `codex-hook-ingress`: Dockerfile, Kubernetes manifests, service/image/config inventory, smoke, runbook и monitoring без служебной БД, migration job и physical `SubmitHookEvent` transport. |

## Текущий бэклог агента #5

| Срез | Что осталось |
|---|---|
| CHI-6b | Persistent ops feed или integration с operations-hub, если понадобится восстановление ленты после рестарта и отдельные retention jobs. |

## Синхронизация с соседними доменами

| Домен или сервис | Что согласовывать |
|---|---|
| `platform-mcp-server` | Hooks не являются MCP tools; MCP видит только tool calls, а hook ingress может фиксировать MCP tool name как безопасное имя в `tool_context`. |
| `agent-manager` | Run/session binding, flow waiting refs, lifecycle events, stop checkpoint, persistent tool/activity timeline и capability context selection. |
| `governance-manager` | Risk assessment, gate request/decision refs и fail-closed/no-decision policy для рискованных `PermissionRequest` и `PreToolUse`. |
| `runtime-manager` | Slot/session binding, workspace refs, materialized capability refs, local emitter/sidecar placement и runtime diagnostics. |
| `provider-hub` | Provider artifact signals, rate-limit hints и hot reconciliation hints без provider payload и stdout `gh`. |
| `interaction-hub` | Delivery request refs, owner feedback, Human gate prompt и callback refs без хранения decision state в interaction. |
| `package-hub` | Package-backed skill source/version/install/manifest refs и manifest digest; ingress не хранит manifest payload, package installation state или `SKILL.md`. |
| `access-manager` | Source binding, actor policy, tool/capability use policy и audit-safe authorization facts. |

## Блокировки и правила работы

- Не добавлять `PreCompact` и `PostCompact` в Codex hook set; compact checkpoints проектируются только как внутренние события `agent-manager`/`runtime-manager`.
- Не переносить MCP `tools/list` или `tools/call` в hook ingress.
- Не создавать skill catalog, package manifest store или materialization state внутри `codex-hook-ingress`.
- Не хранить raw `tool_input`, raw `tool_response`, prompt, stdout/stderr, transcript, session dump, kubeconfig, provider payload или secret values.
- Не создавать persistent историю tool calls/activity внутри ingress; долгосрочная timeline принадлежит `agent-manager`.
- Физический transport `SubmitHookEvent` не выбран: до отдельного решения по gRPC/HTTP разрешён только in-process logical boundary, без proto, OpenAPI и AsyncAPI.
