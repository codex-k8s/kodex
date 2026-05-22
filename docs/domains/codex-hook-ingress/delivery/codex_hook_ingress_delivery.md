---
doc_id: DLV-CK8S-CODEX-HOOK-INGRESS
type: delivery-plan
title: kodex - поставка codex-hook-ingress
status: active
owner_role: EM
created_at: 2026-05-22
updated_at: 2026-05-22
related_issues: [698, 753]
related_prs: []
related_docsets:
  - docs/domains/codex-hook-ingress/product/requirements.md
  - docs/domains/codex-hook-ingress/architecture/design.md
  - docs/domains/codex-hook-ingress/architecture/data_model.md
  - docs/domains/codex-hook-ingress/architecture/api_contract.md
approvals:
  required: ["Owner"]
  status: pending
  request_id: "owner-2026-05-22-codex-hook-ingress-docs"
---

# Поставка codex-hook-ingress

## TL;DR

`codex-hook-ingress` поставляется малыми срезами: сначала доменный пакет документации, затем схемы normalized envelope, hook emitter/sidecar, сервисный каркас ingress, маршрутизация владельцам, permission bridge, realtime/metrics и только потом расширение вокруг skills capability context. Код, proto и AsyncAPI не входят в текущий docs-first срез.

## Входные артефакты

| Документ | Путь |
|---|---|
| Требования | `docs/domains/codex-hook-ingress/product/requirements.md` |
| Дизайн | `docs/domains/codex-hook-ingress/architecture/design.md` |
| Модель данных и состояния | `docs/domains/codex-hook-ingress/architecture/data_model.md` |
| API overview | `docs/domains/codex-hook-ingress/architecture/api_contract.md` |
| Карта Issue | `docs/delivery/issue-map/domains/codex-hook-ingress.md` |
| Сквозная рамка hooks/skills | `docs/platform/architecture/codex_hooks_and_skills.md` |
| MCP и взаимодействия | `docs/platform/architecture/mcp_and_interaction_model.md` |

## Срезы поставки

| Срез | Issue | Результат |
|---|---:|---|
| CHI-0 | #698 | Доменная документация `codex-hook-ingress`: требования, дизайн, модель состояния, API overview, delivery-план и карта Issue. Код, proto, OpenAPI и AsyncAPI не входят. |
| CHI-1 | не назначено | Машинные схемы normalized hook envelope и sanitizer contract; выбор транспорта ingress отдельно от MCP. |
| CHI-2 | не назначено | Hook emitter или local sidecar: чтение Codex hook JSON, redaction, size limits, retry buffer, безопасная отправка. |
| CHI-3 | не назначено | Сервисный каркас `codex-hook-ingress`: process, config, health/readiness, metrics, source verifier, sanitizer, idempotency. |
| CHI-4 | не назначено | Routes к `agent-manager`, `runtime-manager`, `provider-hub`, `interaction-hub` для safe events без бизнес-состояния в ingress. |
| CHI-5 | не назначено | `PermissionRequest` и policy-controlled `PreToolUse` bridge к gate/decision у `agent-manager` и delivery через `interaction-hub`. |
| CHI-6 | не назначено | Realtime/ops feed, retention, sanitizer metrics, rate limits, backpressure и operator diagnostics. |
| CHI-7 | не назначено | Capability context refs для skills: связь с `package-hub`, выбором `agent-manager` и materialization `runtime-manager`; без skill catalog в ingress. |
| CHI-8 | не назначено | Deploy-контур: Dockerfile, Kubernetes manifests, migration job только если нужна служебная БД, smoke, runbook и monitoring. |

## Зависимости и блокировки

| Домен или сервис | Связь | Статус |
|---|---|---|
| `platform-mcp-server` | Отдельная MCP-поверхность tools. | CHI-0 фиксирует разделение; hook ingress не добавляет MCP transport. |
| `agent-manager` | Владеет run/session/gate/decision и выбором skills. | CHI-4/CHI-5 требуют согласованных операций приёма lifecycle/gate signals. |
| `runtime-manager` | Владеет slot, workspace, materialization skills и runtime diagnostics. | CHI-2/CHI-4/CHI-7 требуют runtime context и подготовку emitter/sidecar. |
| `provider-hub` | Владеет provider artifacts, limits и reconciliation. | CHI-4 требует safe provider artifact signal contract без stdout `gh`. |
| `interaction-hub` | Владеет owner feedback, approvals и notifications. | CHI-5 требует delivery request/decision callback contract. |
| `package-hub` | Владеет package source/version/install/manifest для package-backed skills. | CHI-7 использует только package refs и manifest snapshots, не переносит их в ingress. |
| `access-manager` | Владеет правами actor/source/tool/capability. | CHI-3/CHI-5 требуют проверки source и role policy. |

## Критерии начала кода

- CHI-0 принят как доменный docs-first пакет.
- Для каждого кодового среза есть отдельный GitHub Issue.
- Выбран и согласован транспорт `SubmitHookEvent`.
- Машинная схема normalized envelope согласована отдельно от MCP tools.
- Для sanitizer есть список forbidden fields, size limits и тестовые примеры без секретов.
- Старый код из `deprecated/**` не используется как основа реализации.
- Реализация не меняет `platform-mcp-server`, `agent-manager`, `package-hub` или соседние сервисы без отдельного среза владельца.

## Критерии завершения MVP

- Hook emitter или sidecar принимает Codex hook JSON и отправляет только normalized/sanitized envelope.
- `codex-hook-ingress` принимает MVP events: `SessionStart`, `UserPromptSubmit`, `PreToolUse`, `PermissionRequest`, `PostToolUse`, `Stop`.
- Source/run/session/slot binding проверяется до маршрутизации.
- Raw secrets, raw tool input/output, большие stdout/stderr, transcript и session dumps не попадают в ingress storage, logs, metrics и downstream payload.
- `PermissionRequest` проходит через `agent-manager` gate и `interaction-hub` delivery, если требуется человек.
- `PostToolUse` может передать provider artifact signal в `provider-hub` без provider payload.
- Realtime UI получает короткую безопасную ленту событий.
- Skills доступны как refs на выбранный capability context; каталог, manifest и materialization остаются у `package-hub`, `agent-manager` и `runtime-manager`.

## Риски

| Риск | Митигирующее решение |
|---|---|
| Ingress станет лог-хранилищем агента. | Хранить только safe summary, digest, refs и bounded errors; raw logs только у владельцев с retention. |
| Hook events смешаются с MCP tools. | Контрактно запретить `tools/list`, `tools/call` и MCP discovery в ingress. |
| Skills станут отдельным локальным catalog. | Разрешить только `capability_context_ref` and `skill_ref`; source/version/manifest остаются у `package-hub` и выбор у `agent-manager`. |
| Emitter начнёт принимать бизнес-решения. | Emitter только нормализует и отправляет; решения принимает сервис-владелец. |
| Permission timeout продолжит рискованное действие. | Fail-closed policy для risky requests. |

## Апрув

- request_id: `owner-2026-05-22-codex-hook-ingress-docs`
- Решение: pending
- Комментарий: план фиксирует CHI-0 как docs-first срез #698 без реализации и контрактных файлов.
