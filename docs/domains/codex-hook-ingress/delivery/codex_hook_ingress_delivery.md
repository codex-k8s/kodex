---
doc_id: DLV-CK8S-CODEX-HOOK-INGRESS
type: delivery-plan
title: kodex - поставка codex-hook-ingress
status: active
owner_role: EM
created_at: 2026-05-22
updated_at: 2026-05-27
related_issues: [698, 753, 778, 786, 793, 808, 823, 836, 844, 854, 868, 322, 834]
related_prs: []
related_docsets:
  - docs/domains/codex-hook-ingress/product/requirements.md
  - docs/domains/codex-hook-ingress/architecture/design.md
  - docs/domains/codex-hook-ingress/architecture/data_model.md
  - docs/domains/codex-hook-ingress/architecture/api_contract.md
  - docs/domains/codex-hook-ingress/architecture/emitter_sidecar_contract.md
approvals:
  required: ["Owner"]
  status: pending
  request_id: "owner-2026-05-22-codex-hook-ingress-docs"
---

# Поставка codex-hook-ingress

## TL;DR

`codex-hook-ingress` поставляется малыми срезами: сначала доменный пакет документации, затем machine-readable схемы normalized envelope и sanitizer contract, hook emitter/sidecar runtime contract, сервисный каркас ingress, маршрутизация владельцам, permission bridge, realtime/metrics, расширение вокруг skills capability context и deploy-контур. Сервис допускает in-process logical boundary, но proto, OpenAPI, AsyncAPI и physical transport остаются отдельным решением.

## Входные артефакты

| Документ | Путь |
|---|---|
| Требования | `docs/domains/codex-hook-ingress/product/requirements.md` |
| Дизайн | `docs/domains/codex-hook-ingress/architecture/design.md` |
| Модель данных и состояния | `docs/domains/codex-hook-ingress/architecture/data_model.md` |
| API overview | `docs/domains/codex-hook-ingress/architecture/api_contract.md` |
| Контракт hook emitter/local sidecar | `docs/domains/codex-hook-ingress/architecture/emitter_sidecar_contract.md` |
| Runbook | `docs/domains/codex-hook-ingress/ops/codex_hook_ingress_runbook.md` |
| Monitoring | `docs/domains/codex-hook-ingress/ops/codex_hook_ingress_monitoring.md` |
| JSON Schema CHI-1/CHI-2 | `specs/jsonschema/codex-hook-ingress.v1/**` |
| Карта Issue | `docs/delivery/issue-map/domains/codex-hook-ingress.md` |
| Сквозная рамка hooks/skills | `docs/platform/architecture/codex_hooks_and_skills.md` |
| MCP и взаимодействия | `docs/platform/architecture/mcp_and_interaction_model.md` |

## Срезы поставки

| Срез | Issue | Результат |
|---|---:|---|
| CHI-0 | #698 | Доменная документация `codex-hook-ingress`: требования, дизайн, модель состояния, API overview, delivery-план и карта Issue. Код, proto, OpenAPI и AsyncAPI не входят. |
| CHI-1 | #778 | JSON Schema `normalized-hook-envelope.v1` и `sanitizer-contract.v1`, safe examples, validation command и явное разделение hook envelope, MCP tools и business commands. |
| CHI-2 | #786 | Контракт hook emitter/local sidecar: runtime role, чтение Codex hook JSON из `stdin`, sanitizer до buffer/send, logical `SubmitHookEvent`, auth, idempotency, ordering, retry, bounded buffer, backpressure и failure policy без выбора physical transport. |
| CHI-3 | #793 | Сервисный каркас `codex-hook-ingress`: process, config, graceful shutdown, health/readiness/metrics, in-process logical `SubmitHookEvent`, source verifier placeholder, schema validation hook, sanitizer boundary, idempotency repository stub без raw payload storage. |
| CHI-4 | #808 | Route registry и dispatch безопасных частей events через owner ports/stubs к `agent-manager`, `runtime-manager`, `provider-hub`, `governance-manager`, `interaction-hub` и operations/realtime placeholder без бизнес-состояния в ingress. |
| CHI-4b | #844 | Маршрутизация sanitized `PreToolUse`/`PostToolUse` в typed owner port `agent-manager.RecordAgentActivity`: safe activity record содержит refs, tool metadata, status/timestamps, digest, `PostToolUse` `exit_status`/`output_digest`, bounded error, capability refs и correlation/idempotency trace; ingress остаётся sanitizer/router/realtime feed и не хранит persistent tool history. |
| CHI-5 | #836 | `PermissionRequest` и policy-controlled `PreToolUse` bridge к gate/decision у `governance-manager`, ожиданию flow у `agent-manager` и delivery через `interaction-hub`; ingress держит только safe context, idempotency и bounded diagnostics без persistent tool/activity timeline. |
| CHI-6a | #823 | Bounded in-memory realtime/ops feed, retention TTL/capacity, sanitizer metrics, route diagnostics, fixed-window rate limits, safe backpressure и operator diagnostics без служебной БД. |
| CHI-6b | не назначено | Persistent ops feed или integration с operations-hub, если требуется восстановление ленты после рестарта, отдельные retention jobs и SRE runbook. |
| CHI-7 | #854 | Capability context refs для skills подготовлены: normalized envelope, sanitizer boundary и activity route переносят только refs/digests для `package-hub`, выбора `agent-manager` и materialization `runtime-manager`; skill catalog, manifest store и workspace materialization state не входят в ingress. |
| CHI-8 | #868 | Deploy-контур подготовлен: Dockerfile, Kubernetes manifests, service/image/config inventory, smoke, runbook и monitoring. Служебная БД и migration job не создаются, потому что текущая реализация использует bounded in-memory diagnostics и stub repositories без persistent ingress state. |

## Зависимости и блокировки

| Домен или сервис | Связь | Статус |
|---|---|---|
| `platform-mcp-server` | Отдельная MCP-поверхность tools. | CHI-0 фиксирует разделение; hook ingress не добавляет MCP transport. |
| `agent-manager` | Владеет run/session, ожиданием flow, persistent tool/activity timeline и выбором skills. | CHI-4 отправляет только safe projections через owner port; CHI-4b маршрутизирует sanitized tool activity в `RecordAgentActivity`; CHI-5 передаёт safe decision context и refs без хранения долгосрочной истории tool calls в ingress. |
| `runtime-manager` | Владеет slot, workspace, materialization skills и runtime diagnostics. | CHI-2 фиксирует runtime placement, endpoint ref и auth policy для emitter/sidecar; CHI-4 отправляет runtime-safe refs, CHI-7 переносит только materialization refs/digests без workspace paths. |
| `provider-hub` | Владеет provider artifacts, limits и reconciliation. | CHI-4 отправляет только safe provider artifact signal/rate limit parts без stdout `gh` и provider payload. |
| `governance-manager` | Владеет risk assessment, gate request/decision и policy-based approvals. | CHI-5 вызывает owner decision port/stub для `PermissionRequest` и рискованных `PreToolUse`; unavailable/timeout даёт safe result по policy. |
| `interaction-hub` | Владеет owner feedback, approvals, Human gate delivery и notifications. | CHI-5 передаёт safe request context через owner port/stub без владения decision state. |
| `package-hub` | Владеет package source/version/install/manifest для package-backed skills. | CHI-7 использует только package/source/version/install refs и manifest digest, не переносит catalog, manifest payload или package installation state в ingress. |
| `access-manager` | Владеет правами actor/source/tool/capability. | CHI-3/CHI-5 требуют проверки source и role policy. |

## Критерии начала кода

- CHI-0 принят как доменный docs-first пакет.
- Для каждого кодового среза есть отдельный GitHub Issue.
- Для физической business-поверхности выбран и согласован транспорт `SubmitHookEvent`; сервисный каркас CHI-3 допускает только in-process logical boundary без HTTP/gRPC handler, proto, OpenAPI и AsyncAPI.
- JSON Schema normalized envelope согласована отдельно от MCP tools и transport contract.
- Для sanitizer есть machine-readable contract со списком forbidden fields, size limits, redaction, digest/preview правилами и safe examples без секретов.
- Для emitter/sidecar есть machine-readable runtime config с supported events, delivery, auth, idempotency, ordering, retry, buffer, backpressure и failure policy.
- Старый код из `deprecated/**` не используется как основа реализации.
- Реализация не меняет `platform-mcp-server`, `agent-manager`, `package-hub` или соседние сервисы без отдельного среза владельца.

## Критерии завершения MVP

- Hook emitter или sidecar принимает Codex hook JSON и отправляет только normalized/sanitized envelope.
- `codex-hook-ingress` принимает MVP events: `SessionStart`, `UserPromptSubmit`, `PreToolUse`, `PermissionRequest`, `PostToolUse`, `Stop`.
- Source/run/session/slot binding проверяется до маршрутизации.
- Raw secrets, raw tool input/output, большие stdout/stderr, transcript и session dumps не попадают в ingress storage, logs, metrics и downstream payload.
- `PermissionRequest` проходит через `governance-manager` gate, ожидание flow у `agent-manager` и `interaction-hub` delivery, если требуется человек.
- `PostToolUse` может передать provider artifact signal в `provider-hub` без provider payload.
- Realtime UI получает короткую безопасную ленту событий, а persistent история действий для восстановления экрана строится из `agent-manager.AgentActivity`.
- Skills доступны как refs/digests на выбранный capability context; каталог, manifest payload, package installation state и materialization остаются у `package-hub`, `agent-manager` и `runtime-manager`.
- Deploy-контур содержит image build, Kubernetes `Deployment/Service/ConfigMap`, health/readiness/metrics probes, smoke и runbook/monitoring без добавления physical `SubmitHookEvent` transport.

## Риски

| Риск | Митигирующее решение |
|---|---|
| Ingress станет лог-хранилищем агента. | Хранить только короткую ops/realtime feed; persistent safe timeline передавать в `agent-manager`, raw logs только у владельцев с retention. |
| Hook events смешаются с MCP tools. | Контрактно запретить `tools/list`, `tools/call` и MCP discovery в ingress. |
| Skills станут отдельным локальным catalog. | Разрешить только `capability_context_ref`, `skill_ref`, source/package/version refs и digest; source/version/manifest payload остаются у `package-hub`, выбор у `agent-manager`, materialization у `runtime-manager`. |
| Emitter начнёт принимать бизнес-решения. | Emitter только нормализует и отправляет; решения принимает сервис-владелец. |
| Permission timeout продолжит рискованное действие. | Fail-closed policy для risky requests. |

## Апрув

- request_id: `owner-2026-05-22-codex-hook-ingress-docs`
- Решение: pending
- Комментарий: план фиксирует CHI-0 как docs-first срез #698 без реализации и контрактных файлов.
