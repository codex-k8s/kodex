---
doc_id: DLV-CK8S-RISK-GOVERNANCE
type: delivery-plan
title: kodex — поставка governance-manager
status: active
owner_role: EM
created_at: 2026-05-22
updated_at: 2026-05-29
related_issues: [322, 380, 769, 790, 802, 815, 827, 845, 856, 869, 886, 907, 919, 957, 972, 976]
related_prs: []
related_docsets:
  - docs/domains/risk-and-release-governance/product/requirements.md
  - docs/domains/risk-and-release-governance/architecture/design.md
  - docs/domains/risk-and-release-governance/architecture/data_model.md
  - docs/domains/risk-and-release-governance/architecture/api_contract.md
  - docs/domains/risk-and-release-governance/ops/governance_manager_runbook.md
  - docs/domains/risk-and-release-governance/ops/governance_manager_monitoring.md
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
| GOV-6 | #845 | Release decision package, release decision, blocking signal и release safety-loop state готовы без UI/gateway. |
| GOV-7a | #856 | Release decision package явно хранит safe integration refs/summaries соседних доменов без переноса владения project/provider/agent/runtime/interaction state. |
| GOV-7b | #869 | Release package enrichment готов: локальные governance refs валидируются и обогащаются ограниченным snapshot, внешние refs остаются explicit с safe summary diagnostic до service-client срезов. |
| GOV-7c | #886 | Review signal refs intake готов: provider/agent/interaction evidence refs принимаются как safe owner-domain refs, запись проверяет `governance.signal.record`, повтор по source fingerprint не создаёт дубль. |
| GOV-7d | #919 | Потребитель provider review signal готов: `provider.comment.synced` с approved/changes_requested преобразуется в локальный review signal через `libs/go/eventconsumer` без чтения БД/API `provider-hub`. |
| GOV-7e | #930 | Потребитель interaction gate decision готов: `interaction.request.response_recorded` для `owner_service=governance_manager` и `human_gate` преобразуется в локальный `SubmitGateDecision` по safe refs/outcome/digest без чтения БД/API `interaction-hub`. |
| GOV-7f | без отдельного Issue | Runtime/deploy evidence refs принимаются через `RecordReleaseRuntimeEvidence`: release package дозаписывает только безопасные runtime refs, короткие сводки, status/error_code/digest/version, без чтения Kubernetes, БД `runtime-manager` или deploy scripts. |
| GOV-7g | без отдельного Issue | Поверхность чтения runtime/deploy evidence готова для интерфейса владельца и персонала: `GetReleaseDecisionPackage` и `ListReleaseDecisionPackages` возвращают safe runtime refs, status, summary, `error_code`, timestamps, digest/version и связи с gate и release candidate; повтор с тем же fingerprint идемпотентен, конфликтующий fingerprint и устаревший status отклоняются. |
| GOV-7h | #957 | Agent evidence refs принимаются через `RecordReleaseAgentEvidence`: release package дозаписывает только безопасные agent session/run/stage/acceptance/human gate refs, runtime job refs и локальные governance review/gate refs с status/summary/digest/timestamp/version, без чтения БД `agent-manager`, prompt/transcript, stdout/stderr, логов и workspace paths. |
| GOV-7i | #972 | Потребитель agent acceptance evidence готов: `agent.acceptance.completed`/`failed` с явным `governance_release_decision_package_ref` дозаписываются в release package через `RecordReleaseAgentEvidence`; события без package ref подтверждаются без записи, без implicit lookup по project/run и без чтения БД `agent-manager`. |
| GOV-7j | #976 | Сводка чтения governance готова: `GetGovernanceSummary` возвращает безопасную модель чтения для владельца и персонала по target/project/release/package/integration ref с pending/completed decisions, risk class, gate/release outcomes, linked provider/agent/runtime evidence refs и partial diagnostics без доменной логики в gateway. |
| GOV-MCP-1 | без отдельного Issue | MCP-инструмент `governance.summary.get` готов: `platform-mcp-server` вызывает `GetGovernanceSummary` по одному selector и возвращает безопасную сводку для manager/slot-агентов без хранения governance state. |
| GOV-SEC-1 | #380 | Live security/governance summary baseline готов: `GetGovernanceSummary.status` отдаёт общий attention, максимальный риск, счётчики pending/blocked/completed решений, открытых gates, активных blocking signals, evidence, diagnostics, `summary_code` и `next_action_code`; `governance.summary.get` возвращает эти поля без вычисления правил в MCP. Сканеры зависимостей, container images и runtime/infra probes остаются следующими срезами #380. |
| GOV-SEC-2 | #380 | Self-deploy gate baseline готов: safe deploy-plan factors для merge в self-adoption repo классифицируют `services.yaml`, deploy manifests, DB/migration, runtime/runner, gateway, provider write, release policy и RBAC как owner gate risk, блокируют secret/kubeconfig/auth bypass признаки как `R3`, а summary показывает `required_gate_count`, `pending_required_gate_count` и `next_action_code=request_governance_gate`. |
| GOV-7 | не назначено | Интеграции с `agent-manager`, `provider-hub`, `interaction-hub`, `runtime-manager`, `project-catalog` и `operations-hub` подключены через согласованные контракты. |
| GOV-8 | без отдельного Issue | Эксплуатационный контур для первого backend deploy готов: Dockerfile, Kubernetes manifests, migration Job, env/secret inventory, проверка готовности, runbook и monitoring. Operator projections остаются отдельным operations-срезом. |
| GOV-9 | #907 | Event-driven/read-model основа готова: `governance.*` decision lifecycle события несут safe metadata/refs/summary/idempotency correlation для consumers через `platform-event-log`, а authoritative lookup остаётся через gRPC. |

## MVP-порядок

1. Документы и контракты: зафиксировать доменную границу, data model, gRPC/AsyncAPI и события.
2. Сервисный каркас и правила: поднять `governance-manager`, storage, risk profiles, rule evaluation и outbox.
3. Интеграции: подключить role signals от `agent-manager`, provider refs из `provider-hub`, delivery через `interaction-hub`, job/postdeploy signals от `runtime-manager` и project/release policy refs из `project-catalog`.

Этот порядок сохраняет правило: код, proto и AsyncAPI появляются только после согласования стартового документационного пакета, а сервисная бизнес-реализация начинается после контрактного среза.

## Таблица реализации

| Область | Статус | Срез |
|---|---|---|
| Доменная документация | Готова как стартовый пакет домена. | GOV-0 |
| gRPC-контракт `proto/kodex/governance/v1/governance_manager.proto` | Готов; покрывает risk profiles/rules, assessments/factors, review signals, gate lifecycle, release decision package/decision, blocking signals, safety-loop и explicit release integration refs. | GOV-7a |
| Go-код protobuf `proto/gen/go/kodex/governance/v1/**` | Сгенерирован из proto; вручную не правится. | GOV-1 |
| AsyncAPI `specs/asyncapi/governance-manager.v1.yaml` | Готов; фиксирует события `governance.*` через outbox envelope. | GOV-1 |
| Go-контракт событий `libs/go/platformevents/governance/events.gen.go` | Сгенерирован из AsyncAPI; вручную не правится. | GOV-1 |
| Access actions | Добавлены в общий каталог для policy, risk, signal, gate и release операций. | GOV-1 |
| Сервисный процесс, env, health/readiness/metrics и gRPC registration | Готовы как runnable skeleton без deploy-manifests. | GOV-2 |
| gRPC handlers | Поддержанные storage, gate lifecycle, risk evaluator, release decision, safety-loop и release integration refs операции используют доменный сервис и repository; локальный release package enrichment включён в build path. | GOV-7b |
| Repository interfaces/stubs и MVP storage shapes | Stub заменён PostgreSQL repository для risk profile/version, assessment/factors, review signals, gate request/decision, release decision package, command result и outbox. | GOV-3 |
| Storage, migrations и outbox publisher | MVP-миграции и service-local outbox готовы; event-log dispatch подключается через shared outbox runtime. | GOV-3 |
| Gate request/decision lifecycle и access checks | Готовы для `request/read/list/decision/cancel/expire`; delivery/callback orchestration остаётся у `interaction-hub`. | GOV-4 |
| Risk classifier и policy evaluator | Готовы для локальных rules, safe summaries/refs, matched rule refs, required gates, идемпотентного replay, expected version и safe outbox events. Для self-deploy tags `services_yaml`, deploy manifests, DB/migration, runtime/runner, gateway, provider write, release policy и RBAC дают owner gate risk, а secret/kubeconfig/auth bypass признаки блокируют deploy как `R3`. | GOV-5 / GOV-SEC-2 |
| Review signal refs intake | Готов для provider review/comment/check refs, agent run/session/acceptance refs и interaction decision/callback refs через typed `evidence_refs`; `governance-manager` хранит только signal projection metadata, нормализует refs, проверяет access и дедуплицирует повтор по source fingerprint. | GOV-7c |
| Потребитель provider review signal | Готов для `provider.comment.synced`: approved/changes_requested provider review evidence превращается в локальный `RecordReviewSignal`, остальные review states ack-игнорируются, конфликтующий fingerprint poisonится без retry loop. | GOV-7d |
| Потребитель interaction gate decision | Готов для `interaction.request.response_recorded`: обрабатываются только answered Human gate responses для `owner_service=governance_manager`, локального gate request ref и `response_action=approve/reject`; остальные владельцы подтверждаются без записи, неподдержанные action получают безопасный permanent diagnostic. | GOV-7e |
| Потребитель agent acceptance evidence | Готов для `agent.acceptance.completed` и `agent.acceptance.failed`: обрабатываются только terminal acceptance events с явным `governance_release_decision_package_ref`; release package обновляется через `RecordReleaseAgentEvidence`, остальные события подтверждаются без записи. | GOV-7i |
| Release decision lifecycle и safety-loop | Готовы для package build/read/list, decision request/submit/read/list, blocking signals и текущего safety-loop state на safe refs/summaries. | GOV-6 |
| Release integration refs | Готовы для project/repository/release line, provider Issue/PR/check/review, agent run/acceptance, runtime job/deploy, local risk assessment и gate refs с bounded summaries/status/digest/timestamps/version; локальные governance refs обогащаются из repository, внешние refs получают safe summary diagnostic при отсутствии owner summary. | GOV-7b |
| Сводка чтения governance | `GetGovernanceSummary` собирает безопасную модель чтения из локальных risk/gate/review/release/blocking/safety-loop фактов по target/project/release/package/integration ref; gRPC consumers и `platform-mcp-server` получают готовые `status`, required gate counts, pending/completed decisions и evidence summaries без вычисления governance-правил. Для self-deploy risk assessment без созданного gate request summary отдаёт `pending_required_gate_count` и `next_action_code=request_governance_gate`; после `PrepareSelfDeployPlanGate` summary видит уже созданный gate request/decision и не предлагает дубли. HTTP DTO в `staff-gateway` для новых полей остаётся отдельным срезом `console-and-operations-ux`. | GOV-7j / GOV-MCP-1 / GOV-SEC-1 / GOV-SEC-2 / GOV-SEC-3 |
| Эксплуатационный контур | Готов для первого backend deploy: service/migrations Dockerfile stages, Kubernetes ServiceAccount/Service/Deployment/Job, runtime env/secret inventory, PostgreSQL database bootstrap, проверка готовности, runbook и monitoring. | GOV-8 |
| Event-driven/read-model основа | `governance.*` события для risk assessment, review signal, gate, blocking signal, release package/decision и safety-loop публикуют safe ids/refs/status/outcome/reason/safe summary/actor/request/idempotency metadata для consumers через `platform-event-log`. | GOV-9 |
| Интеграции с `agent-manager`, `provider-hub`, `interaction-hub`, `runtime-manager` и `project-catalog` | Provider review events, interaction Human gate response events и agent acceptance terminal events с явной release package ссылкой подключены как безопасные потребители событий; runtime/deploy и agent acceptance/review/runtime evidence refs принимаются через команды governance, когда вызывающая сторона уже знает release package; чтение release package возвращает безопасный evidence-снимок для интерфейса владельца и персонала; service-client чтения, provider write, delivery callbacks и deploy orchestration не реализованы; governance связывает безопасные refs без владения чужой доменной истиной. | GOV-7 |

## Синхронизация с соседними доменами

| Домен | Когда синхронизироваться | Причина |
|---|---|---|
| `projects-and-repositories` | Перед GOV-1 и GOV-5 | Нужны project/repository refs, services policy, branch rules, release policy, release line и risk profile refs без копирования проектной policy. |
| `agent-orchestration` | Перед GOV-1, GOV-5 и GOV-7 | Нужны run/session/acceptance refs, role signals и ожидание governance decision. Agent evidence в release package принимается explicit refs через `RecordReleaseAgentEvidence`; прямой consumer `agent.acceptance.completed`/`failed` работает только при явном `governance_release_decision_package_ref`, а review/risk signal consumer ждёт typed governance outcome. |
| `provider-native-work-items` | Перед GOV-4 и GOV-5 | Нужны provider projections, changed file summary, comments/reviews/check refs и gate ref validation для provider writes. |
| `runtime-and-fleet` | Перед GOV-7 | Runtime/deploy refs принимаются через `RecordReleaseRuntimeEvidence`; прямой consumer для `runtime.job.*` ждёт безопасную привязку события к governance package/gate ref. |
| `interaction-hub` | Перед GOV-4 | Нужен delivery request/callback контракт для Human gate, reminders и escalation без владения decision state. |
| `access-and-accounts` | Перед GOV-1 и GOV-4 | Нужны actions и проверки прав для policy management, gate decision и release decision. |
| `console-and-operations-ux` | После GOV-7j | `staff-gateway` отдаёт `GET /v1/governance/summary` как тонкий HTTP -> gRPC adapter к `GetGovernanceSummary`; добавление `status` rollup в HTTP DTO и подключение экрана в `web-console` остаются отдельными frontend/gateway-срезами. |
| `platform-mcp-server` | После GOV-7j | `governance.summary.get` отдаёт ту же safe summary через MCP для manager/slot-агентов; MCP не хранит state, не читает БД и не подключает frontend. |

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
