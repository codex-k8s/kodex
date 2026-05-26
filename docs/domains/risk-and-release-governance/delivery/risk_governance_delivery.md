---
doc_id: DLV-CK8S-RISK-GOVERNANCE
type: delivery-plan
title: kodex — поставка governance-manager
status: active
owner_role: EM
created_at: 2026-05-22
updated_at: 2026-05-26
related_issues: [322, 769, 790, 802, 815, 827]
related_prs: []
related_docsets:
  - docs/domains/risk-and-release-governance/product/requirements.md
  - docs/domains/risk-and-release-governance/architecture/design.md
  - docs/domains/risk-and-release-governance/architecture/data_model.md
  - docs/domains/risk-and-release-governance/architecture/api_contract.md
approvals:
  required: ["Owner"]
  status: pending
  request_id: "owner-2026-05-22-risk-governance-kickoff"
---

# Поставка governance-manager

## TL;DR

`governance-manager` поставляется малыми срезами: сначала доменный пакет документации и сквозная архитектурная граница, затем транспортные контракты, сервисный каркас и storage, затем risk rules, review signals, gates, release decisions и интеграции с agent/provider/interaction/runtime контурами.

## Входные артефакты

| Документ | Путь |
|---|---|
| Требования домена | `docs/domains/risk-and-release-governance/product/requirements.md` |
| Дизайн домена | `docs/domains/risk-and-release-governance/architecture/design.md` |
| Модель данных | `docs/domains/risk-and-release-governance/architecture/data_model.md` |
| API-обзор | `docs/domains/risk-and-release-governance/architecture/api_contract.md` |
| Карта Issue | `docs/delivery/issue-map/domains/risk-and-release-governance.md` |

## Срезы поставки

| Срез | Issue | Результат |
|---|---|---|
| GOV-0 | #322 | Доменная документация, решение об отдельном `governance-manager`, сквозные границы, README и карта Issue готовы. |
| GOV-1 | #769 | gRPC и AsyncAPI контракты `governance-manager`, события `governance.*`, generated Go contracts и действия доступа готовы; сервисная реализация не входит в срез. |
| GOV-2 | #790 | Сервисный каркас: process, env, health, readiness, metrics, gRPC registration, repository stub и безопасные backlog/`Unimplemented` handlers. |
| GOV-3 | #802 | PostgreSQL-модель MVP-сущностей, repository слой, service-local outbox и gRPC handlers для поддержанных storage-операций готовы. |
| GOV-4 | #815 | Gate request/decision lifecycle готов: request/read/list, submit decision, cancel/expire, access checks, optimistic concurrency, idempotency и безопасные события. |
| GOV-5 | #827 | Risk classifier и policy evaluator работают по входным safe summaries/refs, локальным risk profiles/rules и service/path/API/DB/secret/release/runtime factors без release decision engine. |
| GOV-6 | не назначено | Release decision package, release decision и release safety-loop state готовы без UI/gateway. |
| GOV-7 | не назначено | Интеграции с `agent-manager`, `provider-hub`, `interaction-hub`, `runtime-manager`, `project-catalog` и `operations-hub` подключены через согласованные контракты. |
| GOV-8 | не назначено | Эксплуатационный контур: deploy manifests, migration job, smoke checks, runbook и operator projections. |

## MVP-порядок

1. Документы и контракты: зафиксировать доменную границу, data model, gRPC/AsyncAPI и события.
2. Сервисный каркас и правила: поднять `governance-manager`, storage, risk profiles, rule evaluation и outbox.
3. Интеграции: подключить role signals от `agent-manager`, provider refs из `provider-hub`, delivery через `interaction-hub`, job/postdeploy signals от `runtime-manager` и project/release policy refs из `project-catalog`.

Этот порядок сохраняет правило: код, proto и AsyncAPI появляются только после согласования стартового документационного пакета, а сервисная бизнес-реализация начинается после контрактного среза.

## Таблица реализации

| Область | Статус | Срез |
|---|---|---|
| Доменная документация | Готова как стартовый пакет домена. | GOV-0 |
| gRPC-контракт `proto/kodex/governance/v1/governance_manager.proto` | Готов; покрывает risk profiles/rules, assessments/factors, review signals, gate lifecycle, release decision package/decision, blocking signals и safety-loop. | GOV-1 |
| Go-код protobuf `proto/gen/go/kodex/governance/v1/**` | Сгенерирован из proto; вручную не правится. | GOV-1 |
| AsyncAPI `specs/asyncapi/governance-manager.v1.yaml` | Готов; фиксирует события `governance.*` через outbox envelope. | GOV-1 |
| Go-контракт событий `libs/go/platformevents/governance/events.gen.go` | Сгенерирован из AsyncAPI; вручную не правится. | GOV-1 |
| Access actions | Добавлены в общий каталог для policy, risk, signal, gate и release операций. | GOV-1 |
| Сервисный процесс, env, health/readiness/metrics и gRPC registration | Готовы как runnable skeleton без deploy-manifests. | GOV-2 |
| gRPC handlers | Поддержанные storage, gate lifecycle и risk evaluator операции используют доменный сервис и repository; будущие release/safety-loop операции явно возвращают `Unimplemented`. | GOV-5 |
| Repository interfaces/stubs и MVP storage shapes | Stub заменён PostgreSQL repository для risk profile/version, assessment/factors, review signals, gate request/decision, release decision package, command result и outbox. | GOV-3 |
| Storage, migrations и outbox publisher | MVP-миграции и service-local outbox готовы; event-log dispatch подключается через shared outbox runtime. | GOV-3 |
| Gate request/decision lifecycle и access checks | Готовы для `request/read/list/decision/cancel/expire`; delivery/callback orchestration остаётся у `interaction-hub`. | GOV-4 |
| Risk classifier и policy evaluator | Готовы для локальных rules, safe summaries/refs, matched rule refs, required gates, идемпотентного replay, expected version и safe outbox events. | GOV-5 |
| Release decision engine и safety-loop | Не реализованы. | GOV-6+ |
| Интеграции с `agent-manager`, `provider-hub`, `interaction-hub`, `runtime-manager` и `project-catalog` | Не реализованы; в GOV-1 зафиксированы typed refs и границы. | GOV-7 |

## Синхронизация с соседними доменами

| Домен | Когда синхронизироваться | Причина |
|---|---|---|
| `projects-and-repositories` | Перед GOV-1 и GOV-5 | Нужны project/repository refs, services policy, branch rules, release policy, release line и risk profile refs без копирования проектной policy. |
| `agent-orchestration` | Перед GOV-1, GOV-5 и GOV-7 | Нужны run/session/acceptance refs, role signals и ожидание governance decision. |
| `provider-native-work-items` | Перед GOV-4 и GOV-5 | Нужны provider projections, changed file summary, comments/reviews/check refs и gate ref validation для provider writes. |
| `runtime-and-fleet` | Перед GOV-6 и GOV-7 | Нужны job/deploy/postdeploy/cleanup signals и target environment refs. |
| `interaction-hub` | Перед GOV-4 | Нужен delivery request/callback контракт для Human gate, reminders и escalation без владения decision state. |
| `access-and-accounts` | Перед GOV-1 и GOV-4 | Нужны actions и проверки прав для policy management, gate decision и release decision. |
| `console-and-operations-ux` | После GOV-5 | Нужны read models для operator risk/release state; UI не входит в стартовые срезы. |

## Критерии начала кода

- Принят пакет доменной документации `risk-and-release-governance`.
- Согласована сквозная граница `governance-manager` в `domain_map.md`, `service_boundaries.md` и `data_model.md`.
- Для каждого кодового PR есть отдельный GitHub Issue.
- Контрактный PR создаёт proto и AsyncAPI до сервисной бизнес-реализации.
- Старый код из `deprecated/**` не используется как основа реализации.
- Соседние домены не получают временную risk/release истину ради обхода отсутствующего governance-сервиса.

## Критерии завершения домена

- `governance-manager` имеет свой контур данных, миграций, контрактов и событий.
- Risk profiles, risk assessments, review signals, gate decisions, release decisions и release safety-loop имеют авторитетные команды и чтения.
- Low-risk automation проходит без лишнего Human gate, если policy и checks разрешают переход.
- High-risk transitions, release deploy, rollback/recovery и policy changes не проходят без обязательного evidence и Human gate.
- `interaction-hub` доставляет approvals/callbacks, но decision record остаётся у `governance-manager`.
- `project-catalog`, `agent-manager`, `provider-hub`, `runtime-manager` и `operations-hub` связаны через согласованные контракты.
- Документы и карты Issue обновлены, хвосты перенесены в следующие срезы явно.

## Риски поставки

| Риск | Митигирующее решение |
|---|---|
| Scope растянется до UI/gateway. | UI/gateway вынести в отдельные будущие срезы после read models. |
| Governance начнёт владеть project policy. | В data model и API хранить refs и risk policy, а проектную policy читать из `project-catalog`. |
| Gate delivery смешается с decision state. | Delivery request и callback оставить у `interaction-hub`; decision record хранить в governance. |
| Соседние домены начнут локально решать риск до готовности сервиса. | В GOV-1 зафиксировать контракт и временные `Unimplemented`/blocking outcomes вместо скрытых локальных правил. |

## Апрув

- request_id: `owner-2026-05-22-risk-governance-kickoff`
- Решение: pending
- Комментарий: план поставки фиксирует docs-first старт и порядок MVP-срезов для отдельного `governance-manager`.
