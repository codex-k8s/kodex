---
doc_id: DLV-CK8S-PLATFORM-MCP-SERVER
type: delivery-plan
title: kodex — поставка platform-mcp-server
status: active
owner_role: EM
created_at: 2026-05-14
updated_at: 2026-05-25
related_issues: [747, 753, 760, 771, 780, 698, 322]
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
| MCP-3g | не назначено | Инструменты governance: risk assessment, review signals, gate/release decisions маршрутизируются только через `governance-manager`, delivery/callback — через `interaction-hub`. |
| MCP-4 | #780 | Инструменты provider готовы: маршруты чтения и записи проекций work item, комментариев, связей, artifact signal, операций Issue/PR/comment/review и repository bootstrap/adoption идут только через `provider-hub`; artifact signal не принимает raw JSON payload в MCP-входе; webhook/reconciliation/limits не входят в этот срез. |
| MCP-5 | не назначено | Project/runtime/fleet/package reads и ограниченная диагностика через сервисы-владельцы. |
| MCP-6 | не назначено | Security hardening: actor/source binding, rate limits, backpressure, audit, idempotency и redaction metrics. |
| MCP-7 | не назначено | Deploy-контур: Dockerfile, manifests, migration job только если нужна служебная БД, smoke, runbook и monitoring. |

## Зависимости и блокировки

| Домен или сервис | Связь | Статус |
|---|---|---|
| `agent-manager` | Владеет `Run`, session, flow, role, prompt, acceptance и состоянием ожидания flow. | MCP-3 подключает только готовые операции сессии, `Run`, session snapshot и безопасного чтения. Acceptance, follow-up и Human gate не регистрируются как MCP-инструменты до бизнес-реализации в `agent-manager` и соседних доменах. |
| `governance-manager` | Владеет risk assessment, review signals, gate request/decision, release decision package и release decision. | Инструменты governance требуют отдельного контрактного среза; MCP не хранит decision state и не делает `agent-manager` вторым владельцем gate. |
| `provider-hub` | Владеет чтением, записью и зеркалом provider-данных. | MCP-4 подключает только реализованные операции чтения и записи через `provider-hub`; MCP не ходит в GitHub/GitLab напрямую, не хранит provider-состояние и не возвращает сырой provider payload. |
| `runtime-manager` | Владеет slot, workspace, job и runtime state. | MCP читает и маршрутизирует runtime-инструменты, но не выбирает слот и не исполняет job. |
| `fleet-manager` | Владеет cluster health и placement decisions. | MCP может читать fleet status, но не повторяет placement resolver. |
| `project-catalog` | Владеет project/repository policy и workspace policy. | MCP читает проектную политику только через `project-catalog`; `services.yaml` не парсится в MCP. |
| `package-hub` | Владеет package catalog, installation и manifest. | MCP читает package refs и manifest только через `package-hub`; тексты пакетов не хранятся в MCP. |
| `interaction-hub` | Владеет feedback/approval delivery, callbacks и внешними каналами. | До готовности `interaction-hub` инструменты interaction остаются контрактным заделом; MCP не доставляет уведомления сам и не хранит decision state. |
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
