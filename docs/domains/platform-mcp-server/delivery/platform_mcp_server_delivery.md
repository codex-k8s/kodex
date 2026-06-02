---
doc_id: DLV-CK8S-PLATFORM-MCP-SERVER
type: delivery-plan
title: kodex — поставка platform-mcp-server
status: active
owner_role: EM
created_at: 2026-05-14
updated_at: 2026-06-02
related_issues: [747, 753, 760, 771, 780, 830, 841, 852, 933, 698, 322]
related_prs: []
related_docsets:
  - docs/domains/platform-mcp-server/product/requirements.md
  - docs/domains/platform-mcp-server/architecture/design.md
  - docs/domains/platform-mcp-server/architecture/api_contract.md
  - docs/domains/platform-mcp-server/architecture/contract_strategy.md
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-14-platform-mcp-kickoff"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-14
---

# Поставка platform-mcp-server

## TL;DR

`platform-mcp-server` поставляется малыми срезами: сначала границы и стратегия контрактов, затем сервисный каркас MCP, маршруты к владельцам, безопасность и эксплуатационный контур. Сервис не владеет бизнес-состоянием и не принимает Codex hooks. Hook emitter, локальный sidecar и входной контур Codex hooks поставляются через отдельный сервисный пакет `codex-hook-ingress` и не закрываются этим планом.

## Входные артефакты

| Документ | Путь |
|---|---|
| Требования | `docs/domains/platform-mcp-server/product/requirements.md` |
| Дизайн | `docs/domains/platform-mcp-server/architecture/design.md` |
| API-обзор | `docs/domains/platform-mcp-server/architecture/api_contract.md` |
| Стратегия контрактов MCP и Codex hooks | `docs/domains/platform-mcp-server/architecture/contract_strategy.md` |
| Карта Issue | `docs/delivery/issue-map/domains/platform-mcp-server.md` |

## Срезы поставки

| Срез | Issue | Результат |
|---|---:|---|
| MCP-0 | #747 | Доменный пакет сервисной границы, ответственность, MVP-группы инструментов, безопасность и delivery-план готовы. Код, proto и AsyncAPI не входят. |
| MCP-1 | #753 | Стратегия контрактов готова: MCP-инструменты описываются через MCP SDK, JSON Schema и snapshot-проверки `tools/list`; Codex hooks вынесены в `codex-hook-ingress`; YAML-каталог не является каноникой. |
| MCP-2 | #760 | Сервисный каркас готов: процесс, конфигурация, health/readiness/metrics, MCP Streamable HTTP, проверка bearer-токена, `diagnostics.mcp_status.read`, каталог маршрутов к сервисам-владельцам и snapshot-проверка `tools/list`. Бизнес-маршруты, входной контур hooks, хранилище skills и манифесты выкладки не входят. |
| MCP-3 | #771 | Инструменты `agent-manager` для реализованной поверхности: `agent.session.start`, `agent.run.start`, `agent.run.record_state`, `agent.session.record_snapshot` и `diagnostics.run_context.read` маршрутизируются только через `agent-manager`; acceptance, follow-up и Human gate остаются следующими срезами до готовности владельца. |
| MCP-3g | #830 | Инструменты жизненного цикла gate готовы: `governance.gate.request/get/list/submit_decision/cancel/expire` маршрутизируются только через `governance-manager`, возвращают безопасные ссылки, статусы и сводки и не хранят состояние решений в MCP. Оценка риска и релизные решения закрываются отдельными срезами MCP-3r/MCP-3d; доставка и callback остаются отдельным контуром. |
| MCP-3r | #841 | Инструменты оценки риска готовы: `governance.risk.evaluate/reevaluate/get/list` маршрутизируются только через `governance-manager`, принимают типизированные ссылки и ограниченные сводки, добирают matched rules/factors через типизированное чтение владельца и возвращают assessment refs/status/risk class, matched rule refs/counts, required gate refs, version/timestamps без хранения состояния risk в MCP. |
| MCP-3d | #852 | Инструменты релизных решений готовы: release package prepare/get/list, decision request/submit/get/list, blocking signal record/resolve/list и safety-loop record/get маршрутизируются только через `governance-manager`, возвращают безопасные ссылки, статусы, сводки, счётчики, version/timestamps и не хранят состояние release в MCP. |
| MCP-4 | #780 | Инструменты provider готовы: маршруты чтения и записи проекций work item, комментариев, связей, artifact signal, операций Issue/PR/comment/review и repository bootstrap/adoption идут только через `provider-hub`; artifact signal не принимает raw JSON payload в MCP-входе; webhook/reconciliation/limits не входят в этот срез. |
| MCP-4o | #933 | Инструменты к готовым сервисным поверхностям владельцев готовы: `agent.human_gate.request/get/list` идут через `agent-manager`, `interaction.owner_inbox.list/get/respond` — через `interaction-hub`, `governance.signal.record_review/list_review` — через `governance-manager`; MCP возвращает только безопасные ссылки, статусы, сводки, версии и timestamps. |
| GOV-MCP-1 | без отдельного Issue | Инструмент `governance.summary.get` готов: MCP принимает ровно один selector, вызывает `governance-manager.GetGovernanceSummary` и возвращает безопасную сводку без хранения governance state и без frontend-подключения. |
| GOV-SEC-1 | #380 | `governance.summary.get` возвращает доменно подготовленный live `status` rollup: общий attention, максимальный риск, счётчики pending/blocked/completed решений, открытых gates, активных blocking signals, evidence, diagnostics, `summary_code` и `next_action_code`; MCP не вычисляет governance/security правила. |
| GOV-SEC-2 | #380 | `governance.summary.get` возвращает required gate counts и `next_action_code=request_governance_gate` для self-deploy plan, когда risk assessment уже требует owner/governance gate, но gate request ещё не создан. |
| MCP-5 | не назначено | Project/runtime/fleet/package reads и ограниченная диагностика через сервисы-владельцы. |
| MCP-6 | не назначено | Security hardening: actor/source binding, rate limits, backpressure, audit, idempotency и redaction metrics. |
| MCP-7 | не назначено | Deploy-контур: Dockerfile, manifests, migration job только если нужна служебная БД, runbook и monitoring. |

## Зависимости и блокировки

| Домен или сервис | Связь | Статус |
|---|---|---|
| `agent-manager` | Владеет `Run`, session, flow, role, prompt, acceptance и состоянием ожидания flow. | MCP-3 подключает операции сессии, `Run`, session snapshot и безопасного чтения; MCP-4o подключает `agent.human_gate.request/get/list` к готовому жизненному циклу Human gate. Acceptance и follow-up остаются отдельными срезами. |
| `governance-manager` | Владеет risk assessment, review signals, gate request/decision, release decision package, release decision, blocking signals и release safety-loop. | MCP-3g подключает жизненный цикл gate, MCP-3r подключает оценку риска, MCP-3d подключает релизные решения, MCP-4o подключает запись и чтение review signals, GOV-MCP-1 подключает `governance.summary.get`, GOV-SEC-1 отдаёт live `status` rollup, GOV-SEC-2 отдаёт required gate counts для self-deploy gate из owner-prepared summary. MCP не хранит состояние risk/gate/release/review/summary и не делает `agent-manager` вторым владельцем governance-состояния. |
| `provider-hub` | Владеет чтением, записью и зеркалом provider-данных. | MCP-4 подключает только реализованные операции чтения и записи через `provider-hub`; MCP не ходит в GitHub/GitLab напрямую, не хранит provider-состояние и не возвращает сырой provider payload. |
| `runtime-manager` | Владеет slot, workspace, job и runtime state. | MCP читает и маршрутизирует runtime-инструменты, но не выбирает слот и не исполняет job. |
| `fleet-manager` | Владеет cluster health и placement decisions. | MCP может читать fleet status, но не повторяет placement resolver. |
| `project-catalog` | Владеет project/repository policy и workspace policy. | MCP читает проектную политику только через `project-catalog`; `services.yaml` не парсится в MCP. |
| `package-hub` | Владеет package catalog, installation и manifest. | MCP читает package refs и manifest только через `package-hub`; тексты пакетов не хранятся в MCP. |
| `interaction-hub` | Владеет feedback/approval delivery, callbacks, входящими задачами владельца и внешними каналами. | MCP-4o подключает `interaction.owner_inbox.list/get/respond` к готовым операциям `ListOwnerInboxItems`, `GetOwnerInboxItem` и `RecordInteractionResponse`; MCP не доставляет уведомления сам и не хранит состояние решений. |
| #698 hooks | Hook-события должны войти в MVP. | `platform-mcp-server` не закрывает #698. Hook emitter, sidecar и входной контур реализуются через `codex-hook-ingress`; MCP-сервис может только дать отдельные инструменты чтения или управления, если они нужны агенту. |

## Критерии начала кода

- Принят MCP-0 docset.
- Для каждого кодового PR заведён отдельный GitHub Issue.
- До реализации группы маршрутов есть явно оформленная стратегия MCP-контрактов: Go-регистрация инструментов через MCP SDK, JSON Schema входов и snapshot-проверки `tools/list`.
- Старый код из `deprecated/**` не используется как основа реализации.
- Инструменты записи provider не реализуются без уже готового типизированного provider pipeline.

## Критерии завершения MVP

- `platform-mcp-server` имеет сервисный процесс, наблюдаемость, лимиты и безопасную обработку данных вызова.
- `agent-manager` и slot-агенты могут вызывать разрешённые инструменты через MCP с actor/source/run/slot binding.
- Инструменты provider используют только `provider-hub`.
- Project/runtime/fleet/package reads используют только сервисы-владельцы.
- Диагностика ограничена и не раскрывает секреты, большие логи, kubeconfig, исходные данные провайдера и session dumps.
- Codex hooks принимаются отдельным `codex-hook-ingress`, а не MCP-сервисом.

## Апрув

- request_id: `owner-2026-05-14-platform-mcp-kickoff`
- Решение: approved
- Комментарий: план поставки `platform-mcp-server` согласован как целевое состояние MCP-0.
