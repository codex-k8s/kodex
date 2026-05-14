---
doc_id: DLV-CK8S-PLATFORM-MCP-SERVER
type: delivery-plan
title: kodex — поставка platform-mcp-server
status: active
owner_role: EM
created_at: 2026-05-14
updated_at: 2026-05-14
related_issues: [747, 698]
related_prs: []
related_docsets:
  - docs/domains/platform-mcp-server/product/requirements.md
  - docs/domains/platform-mcp-server/architecture/design.md
  - docs/domains/platform-mcp-server/architecture/api_contract.md
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-14-platform-mcp-kickoff"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-14
---

# Поставка platform-mcp-server

## TL;DR

`platform-mcp-server` поставляется малыми срезами: сначала границы и верхнеуровневые контракты, затем машинный каталог инструментов, сервисный каркас, приём hook-событий, маршруты к владельцам, безопасность и эксплуатационный контур. Сервис не владеет бизнес-состоянием и не закрывает #698 до реализации hooks.

## Входные артефакты

| Документ | Путь |
|---|---|
| Требования | `docs/domains/platform-mcp-server/product/requirements.md` |
| Дизайн | `docs/domains/platform-mcp-server/architecture/design.md` |
| API-обзор | `docs/domains/platform-mcp-server/architecture/api_contract.md` |
| Карта Issue | `docs/delivery/issue-map/domains/platform-mcp-server.md` |

## Срезы поставки

| Срез | Issue | Результат |
|---|---:|---|
| MCP-0 | #747 | Доменный пакет сервисной границы, ответственность, MVP-группы инструментов, безопасность и delivery-план готовы. Код, proto и AsyncAPI не входят. |
| MCP-1 | не назначено | Машинный tool catalog, envelope, политика версионирования MCP-инструментов и тестовые fixtures готовы. |
| MCP-2 | не назначено | Сервисный каркас: процесс, конфигурация, health/readiness/metrics, MCP transport skeleton и dependency clients без бизнес-маршрутов. |
| MCP-3 | не назначено | Приём hook-событий #698, очистка данных вызова, безопасные сводки и маршруты в `agent-manager`, `runtime-manager`, `provider-hub`. |
| MCP-4 | не назначено | Agent-manager tools: session/run/gate/acceptance/follow-up маршруты только через `agent-manager`. |
| MCP-5 | не назначено | Provider tools: типизированные provider read/write маршруты через `provider-hub` без прямого GitHub/GitLab доступа. |
| MCP-6 | не назначено | Project/runtime/fleet/package reads и ограниченная диагностика через сервисы-владельцы. |
| MCP-7 | не назначено | Security hardening: actor/source binding, rate limits, backpressure, audit, idempotency и redaction metrics. |
| MCP-8 | не назначено | Deploy-контур: Dockerfile, manifests, migration job только если нужна служебная БД, smoke, runbook и monitoring. |

## Зависимости и блокировки

| Домен или сервис | Связь | Статус |
|---|---|---|
| `agent-manager` | Владеет `Run`, session, gates, flow, role, prompt и acceptance. | MCP-0 не конфликтует с AGO-3: фиксирует внешний инструментальный слой, а не БД flow/role/prompt. Реальные agent tools требуют готовности соответствующих операций `agent-manager`. |
| `provider-hub` | Владеет provider read/write и зеркалом. | MCP-0 не конфликтует с PRV-8a: provider tools должны идти через существующий write pipeline и будущие provider bootstrap/adoption контракты. |
| `runtime-manager` | Владеет slot, workspace, job и runtime state. | MCP читает и маршрутизирует runtime tools, но не выбирает слот и не исполняет job. |
| `fleet-manager` | Владеет cluster health и placement decisions. | MCP может читать fleet status, но не повторяет placement resolver. |
| `project-catalog` | Владеет project/repository policy и workspace policy. | MCP читает проектную политику только через `project-catalog`; `services.yaml` не парсится в MCP. |
| `package-hub` | Владеет package catalog, installation и manifest. | MCP читает package refs и manifest только через `package-hub`; тексты пакетов не хранятся в MCP. |
| `interaction-hub` | Владеет feedback/approval delivery. | До готовности `interaction-hub` interaction tools остаются контрактным заделом; MCP не доставляет уведомления сам. |
| #698 hooks | Hook-события должны войти в MVP. | MCP-0 фиксирует границу, но #698 остаётся открытым до реализации hook emitter и ingress. |

## Критерии начала кода

- Принят MCP-0 docset.
- Для каждого кодового PR заведён отдельный GitHub Issue.
- До реализации route group есть machine-readable tool catalog или явно оформленный контрактный срез.
- Старый код из `deprecated/**` не используется как основа реализации.
- Provider write tools не реализуются без уже готового typed provider pipeline.

## Критерии завершения MVP

- `platform-mcp-server` имеет сервисный процесс, наблюдаемость, лимиты и безопасную обработку данных вызова.
- Приём hook-событий принимает все события из согласованного набора #698 и не пишет сырые данные вызова.
- `agent-manager` и slot-агенты могут вызывать разрешённые инструменты через MCP с actor/source/run/slot binding.
- Provider tools используют только `provider-hub`.
- Project/runtime/fleet/package reads используют только сервисы-владельцы.
- Диагностика ограничена и не раскрывает секреты, большие логи, kubeconfig, исходные данные провайдера и session dumps.

## Апрув

- request_id: `owner-2026-05-14-platform-mcp-kickoff`
- Решение: approved
- Комментарий: план поставки `platform-mcp-server` согласован как целевое состояние MCP-0.
