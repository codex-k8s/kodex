---
doc_id: API-CK8S-RISK-GOVERNANCE-0001
type: api-contract
title: kodex — API-обзор governance-manager
status: active
owner_role: SA
created_at: 2026-05-22
updated_at: 2026-06-02
related_issues: [322, 380, 769, 790, 815, 827, 845, 856, 869, 886, 907, 919, 957, 972, 976]
related_prs: []
approvals:
  required: ["Owner"]
  status: pending
  request_id: "owner-2026-05-22-risk-governance-kickoff"
---

# API-обзор: governance-manager

## TL;DR

- Тип API: внутренний gRPC `GovernanceManagerService`, доменные события `governance.*`, будущие MCP-инструменты через `platform-mcp-server`.
- Аутентификация: gateway, MCP или сервисный токен; команды управления policy и human/release decisions дополнительно проверяются через `access-manager`.
- Версионирование: стабильное транспортное пространство имён `kodex.governance.v1`.
- Основные операции: risk profiles, risk assessments, review signals, gate requests/decisions, release decision packages, release decisions и safety-loop signals.

## Спецификации

- gRPC proto: `proto/kodex/governance/v1/governance_manager.proto`.
- Сгенерированный Go-контракт: `proto/gen/go/kodex/governance/v1/**`.
- AsyncAPI: `specs/asyncapi/governance-manager.v1.yaml`.
- Сгенерированные Go-контракты событий: `libs/go/platformevents/governance/events.gen.go`.
- MCP-инструменты: публикуются через `platform-mcp-server`, не напрямую из доменного сервиса.
- Внешний HTTP для будущей консоли: через профильный gateway, не напрямую из `governance-manager`.

Этот документ является обзором целевого API. Машинные спецификации являются источником правды транспорта, а документ должен обновляться синхронно с изменением транспортной спецификации.

`EvaluateRisk` и `ReevaluateRisk` не принимают raw diff, provider payload, runtime logs или секреты. Классификатор работает по `ProjectContextRef`, provider/agent/runtime refs, `RiskEvaluationSummary`, typed factors, evidence refs и локально сохранённым risk profiles/rules. Project/repository refs и release policy refs подготавливаются соседними сервисами и передаются в запросе; прямое чтение `project-catalog`, `provider-hub`, GitHub/GitLab или runtime projections не входит в текущий evaluator contract.

## Операции

| Операция | Вид | Доступ | Идемпотентность | Примечание |
|---|---|---|---|---|
| `CreateRiskProfile` | gRPC command | `governance.policy.manage` | `CommandMeta.command_id` | Создаёт профиль риска для scope. |
| `CreateRiskProfileVersion` | gRPC command | `governance.policy.manage` | `command_id` | Создаёт версию правил риска и gate policy. |
| `ActivateRiskProfileVersion` | gRPC command | `governance.policy.manage` | `command_id` + expected version | Активирует версию для новых evaluations. |
| `ArchiveRiskProfile` | gRPC command | `governance.policy.manage` | `command_id` + expected version | Архивирует профиль без удаления истории решений. |
| `GetRiskProfile` | gRPC query | `governance.policy.read` | нет | Читает профиль и активную версию. |
| `GetRiskProfileVersion` | gRPC query | `governance.policy.read` | нет | Читает immutable-версию risk rules и gate policies. |
| `ListRiskProfiles` | gRPC query | `governance.policy.read` | нет | Читает профили по scope/status. |
| `ListRiskRules` | gRPC query | `governance.policy.read` | нет | Читает risk rules конкретной версии профиля. |
| `ListGatePolicies` | gRPC query | `governance.policy.read` | нет | Читает gate policies конкретной версии профиля. |
| `EvaluateRisk` | gRPC command | `governance.risk.evaluate` | `command_id` | Создаёт assessment для transition, PR/MR, release candidate, job или policy change по входным safe summaries/refs и локальным risk profile/rules. |
| `ReevaluateRisk` | gRPC command | `governance.risk.evaluate` | `command_id` + expected version | Пересчитывает assessment после новых safe summaries, signals или policy version. |
| `GetRiskAssessment` | gRPC query | `governance.risk.read` | нет | Читает assessment, factors и required gates. |
| `ListRiskAssessments` | gRPC query | `governance.risk.read` | нет | Читает assessments по project/repository/target/risk class/status. |
| `ListRiskFactors` | gRPC query | `governance.risk.read` | нет | Читает факторы риска assessment без полного diff или логов. |
| `RecordReviewSignal` | gRPC command | `governance.signal.record` | `command_id` + normalized owner evidence refs | Записывает signal от reviewer, QA, lexical gatekeeper, SRE, security или custom role. |
| `ListReviewSignals` | gRPC query | `governance.signal.read` | нет | Читает signals по target или assessment. |
| `RequestGate` | gRPC command | `governance.gate.request` | `command_id` | Создаёт governance gate request и evidence package; delivery request/ref остаётся у `interaction-hub`. |
| `SubmitGateDecision` | gRPC command | `governance.gate.decide` | `command_id` + expected version | Фиксирует решение из UI/provider/external callback после проверки actor policy. |
| `CancelGate` | gRPC command | `governance.gate.decide` | `command_id` + expected version | Переводит открытый gate request в `cancelled`; доставка и callback остаются у `interaction-hub`. |
| `ExpireGate` | gRPC command | `governance.gate.decide` | `command_id` + expected version | Переводит открытый gate request в `expired` после timeout policy или delivery expiry. |
| `GetGateDecision` | gRPC query | `governance.gate.read` | `gate_request_id` | Читает одно final gate decision после fail-closed проверки доступа по gate request. |
| `ListGateDecisions` | gRPC query | `governance.gate.read` | gate request или target | Читает final gate decisions по gate request или target; outcome используется только как уточняющий фильтр. |
| `GetGateRequest` | gRPC query | `governance.gate.read` | нет | Читает gate request, evidence и decision status. |
| `ListGateRequests` | gRPC query | `governance.gate.read` или `governance.risk.read` | target или risk assessment | Читает ожидающие, resolved или просроченные gates по target или assessment; status используется только как уточняющий фильтр. |
| `BuildReleaseDecisionPackage` | gRPC command | `governance.release.prepare` | `command_id` | Собирает release evidence из project/provider/runtime/agent refs, explicit `integration_refs` и optional local `risk_assessment_id`; локальные governance refs обогащаются ограниченным snapshot. |
| `RecordReleaseRuntimeEvidence` | gRPC command | `governance.release.update` | `command_id` или `idempotency_key` + `expected_version` | Дозаписывает в release package безопасные `runtime_refs`, `evidence_refs` и `integration_refs` домена `runtime` после build/deploy/postdeploy факта. |
| `RecordReleaseAgentEvidence` | gRPC command | `governance.release.update` | `command_id` или `idempotency_key` + `expected_version` | Дозаписывает в release package безопасные `agent_context`, `evidence_refs` и `integration_refs` доменов `agent`, `runtime` и `governance` после agent acceptance/review/runtime факта. |
| `GetReleaseDecisionPackage` | gRPC query | `governance.release.read` | нет | Читает release evidence package. |
| `ListReleaseDecisionPackages` | gRPC query | `governance.release.read` | нет | Читает release packages по project/candidate/status. |
| `RequestReleaseDecision` | gRPC command | `governance.release.request` | `command_id` | Запрашивает release gate или автоматическое decision по policy. |
| `SubmitReleaseDecision` | gRPC command | `governance.release.decide` | `command_id` + expected version | Фиксирует go/no-go/hold/rollback/follow-up decision. |
| `GetReleaseDecision` | gRPC query | `governance.release.read` | нет | Читает одно release decision. |
| `ListReleaseDecisions` | gRPC query | `governance.release.read` | нет | Читает release decisions по package/project/status/outcome. |
| `RecordBlockingSignal` | gRPC command | `governance.signal.record` | `command_id` | Фиксирует blocking signal от acceptance, runtime, provider, interaction, monitoring или человека. |
| `ResolveBlockingSignal` | gRPC command | `governance.signal.resolve` | `command_id` + expected version | Закрывает blocking signal с reason. |
| `ListBlockingSignals` | gRPC query | `governance.signal.read` | нет | Читает активные и исторические blocking signals. |
| `RecordReleaseSafetyState` | gRPC command | `governance.release.update` | `command_id` + expected version | Обновляет safety-loop после deploy/postdeploy signal. |
| `GetReleaseSafetyState` | gRPC query | `governance.release.read` | нет | Читает текущее состояние release safety-loop. |
| `GetGovernanceSummary` | gRPC query | существующие `governance.*.read` по выбранному scope | нет | Возвращает доменно подготовленную безопасную сводку для gRPC consumers, `platform-mcp-server` и будущего UI: `status` rollup, pending/completed decisions, risk class, gate/release outcomes и linked provider/agent/runtime evidence refs. |

## Интеграции с другими сервисами

`governance-manager` принимает review/risk/release refs только как safe owner-domain refs и summaries. Для `RecordReviewSignal` обязательны typed `evidence_refs`; provider review/comment/check refs остаются у `provider-hub`, agent run/session/acceptance refs остаются у `agent-manager`, interaction decision/callback refs остаются у `interaction-hub`. Сервис нормализует evidence refs, строит локальный source fingerprint по identity-проекции `kind/ref` и не создаёт новый signal при повторной передаче того же owner-domain ref set; конфликтующие факты с тем же fingerprint отклоняются. Дубли одного `kind/ref` с разной metadata в одном запросе отклоняются до записи.

Первый входящий путь для событий соседнего домена использует стабильное событие `provider.comment.synced` из `provider-hub`. Потребитель события является явным runtime-путём и включается только через `KODEX_GOVERNANCE_MANAGER_PROVIDER_REVIEW_SIGNAL_CONSUMER_ENABLED=true`. `governance-manager` обрабатывает только `review_state=approved` и `review_state=changes_requested`, преобразует provider work item ref и provider comment/comment projection ref в локальный `RecordReviewSignal` и сохраняет только target ref, outcome/severity, ограниченный summary, evidence ref, actor ref, request/event ref и idempotency correlation. `commented`, `pending`, `dismissed` и пустые states подтверждаются без записи, потому что они не выражают governance decision. Сырые comment body, provider webhook body, diff, URL payload и provider API response не читаются и не сохраняются.

Второй входящий путь использует `interaction.request.response_recorded` из `interaction-hub` только как ответ владельца для gate decision, а не как review signal. Потребитель включается только через `KODEX_GOVERNANCE_MANAGER_INTERACTION_GATE_DECISION_CONSUMER_ENABLED=true` и обрабатывает событие, когда `owner_service=governance_manager`, `request_kind=human_gate`, статус request — `answered`, есть `governance_gate_request_ref` или `owner_request_ref` на локальный gate request, а `response_action` однозначно равен `approve` или `reject`. Потребитель читает локальный gate request для expected version, вызывает существующий `SubmitGateDecision` и сохраняет только actor ref, interaction request/response refs, safe source ref, digest summary, outcome, event/request ref и idempotency fingerprint. Сырые response text, callback body, delivery payload, prompt/transcript, logs, workspace paths и secrets не читаются и не сохраняются.

Третий входящий путь использует `agent.acceptance.completed` и `agent.acceptance.failed` из `agent-manager` только как release package evidence, а не как review/risk signal. Потребитель включается только через `KODEX_GOVERNANCE_MANAGER_AGENT_ACCEPTANCE_EVIDENCE_CONSUMER_ENABLED=true` и обрабатывает событие, когда есть явный `governance_release_decision_package_ref`, `acceptance_result_id`, `session_id` и terminal status, согласованный с типом события. Если package ref отсутствует, событие подтверждается без записи: `governance-manager` не делает implicit lookup по project/repository/run. Потребитель читает только локальный release package для expected version, вызывает существующий `RecordReleaseAgentEvidence` и сохраняет agent session/run/stage/acceptance refs, runtime job ref, status, bounded summary, digest, observed timestamp, version и event idempotency fingerprint. Сырые prompt, transcript, raw tool input/output, stdout/stderr, workspace paths, runtime logs, provider payload и secrets не читаются и не сохраняются.

Событие `agent.follow_up.review_signaled` пока не используется как review/risk signal input: в текущем контракте ему не хватает typed governance outcome для безопасного маппинга без чтения owner-домена. `interaction.request.response_recorded` используется только для согласованной command boundary gate decision и не подменяет review signal.

GOV-7b хранит явные safe refs/summaries в release package и выполняет read-validation/enrichment для локальных governance refs: risk assessment, review signal, gate request, gate decision и связанный release package. Для project/provider/agent/runtime refs `governance-manager` сохраняет explicit ref и безопасный diagnostic в `summary`, если вызывающая сторона не передала owner-domain summary; прямые service-client чтения соседних доменов подключаются отдельными интеграционными срезами после согласования read-контрактов и runtime composition.

Для runtime/deploy фактов используется команда `RecordReleaseRuntimeEvidence`. Вызывающая сторона должна уже знать `release_decision_package_id` и передавать только безопасные `RuntimeContextRef`, `EvidenceRef` и `ReleaseIntegrationRef` с `domain=runtime`, `kind=job|deploy|postdeploy|environment|artifact|summary`, ограниченный `status`, короткий `summary`, `digest`, `observed_at`, `version`/etag и опциональный `error_code`. Для `job|deploy|postdeploy` принимаются только статусы `pending`, `claimed`, `running`, `succeeded`, `failed`, `cancelled`, `timed_out`; более старый status-снимок для уже зафиксированного runtime ref отклоняется как устаревший. Команда требует `expected_version`, сохраняет идемпотентный command result, одинаковую повторную доставку с тем же fingerprint/digest не превращает в новое событие, а конфликтующий fingerprint для того же `domain/kind/ref` отклоняет. `GetReleaseDecisionPackage` и `ListReleaseDecisionPackages` возвращают эти runtime/deploy refs как поверхность чтения для интерфейса владельца и персонала: release candidate, связанные gate refs через `integration_refs`, runtime job/deploy/postdeploy refs, status, короткий безопасный `summary`, `error_code`, `observed_at`, `digest`, `version` и версию package без логов и raw payload. События `runtime.job.*` остаются сигналами домена-владельца `runtime-manager`; прямой consumer в governance не включается, пока событие не несёт согласованную безопасную привязку к `release_decision_package_id` или локальному gate/package ref.

Для agent acceptance/review/runtime фактов используется команда `RecordReleaseAgentEvidence`. Вызывающая сторона должна уже знать `release_decision_package_id` и передавать только безопасные `AgentContextRef`, `EvidenceRef` и `ReleaseIntegrationRef` с `domain=agent|runtime|governance`: agent session/run/stage/acceptance/human gate refs, runtime job refs и локальные governance review/gate refs. Для agent refs принимаются только ограниченные статусы: acceptance `pending|waiting|passed|failed|skipped`, run `requested|starting|running|waiting|completed|failed|cancelled`, human gate `requested|waiting|resolved|failed|cancelled`, session `open|waiting|completed|failed|cancelled`; более старый status-снимок для того же `domain/kind/ref` отклоняется как устаревший, конфликтующий digest/version/status/summary отклоняется как конфликт. Команда требует `expected_version`, одинаковая повторная доставка с тем же fingerprint/digest не создаёт новую версию и событие. `governance-manager` не читает БД `agent-manager`, не получает prompt, transcript, raw tool input/output, stdout/stderr, workspace paths или логи; исходная acceptance/run/human gate истина остаётся у `agent-manager`.

`GetGovernanceSummary` — авторитетное чтение для интерфейса владельца, персонала и быстрых manager/slot-агентов поверх уже сохранённого governance state. Запрос обязательно ограничен ровно одним selector: `target`, `project_context`, `release_candidate_ref`, `release_decision_package_id` или безопасным `integration_ref` из release package. Сервис не объединяет независимые selectors в одном ответе, чтобы карточка владельца или агентная сводка не смешивала решения и evidence от разных задач, PR, release package или agent run. Ответ содержит typed `status`, `pending_decisions`, `completed_decisions` и `evidence_summaries`: общий attention, максимальный `risk_class`, счётчики pending/blocked/completed решений, открытых gates, активных blocking signals, evidence и diagnostics, `summary_code`, `next_action_code`, risk assessment, review signal outcome, gate request/decision, release package/decision, blocking signal, safety-loop state, provider refs, agent/runtime refs, короткие safe summaries, timestamps, digest/version и bounded diagnostics для отсутствующих локальных связей. Если соседний owner-domain ref ещё не загружен или не имеет service-client read-contract, summary возвращает partial response с explicit ref и диагностикой вместо ошибки; прямого lookup по project/run/provider payload нет. `platform-mcp-server` отдаёт owner-prepared `status` и summary через `governance.summary.get`: MCP валидирует один selector, передаёт actor/source/request context, вызывает gRPC и не хранит governance state. `staff-gateway` сохраняет тонкую HTTP -> gRPC границу для существующего `GET /v1/governance/summary`; добавление нового `status` в HTTP DTO и frontend остаётся отдельным срезом `console-and-operations-ux`.

Минимальная связь с security baseline строится через уже существующие governance факты: vulnerability или runtime/infra finding передаётся как bounded risk factor, review signal, blocking signal или release evidence ref, после чего summary показывает максимальный риск, блокирующие решения, открытые gates, активные blocking signals и безопасные diagnostics. Сканеры зависимостей, container images и runtime/infra probes остаются владельцами своих исходных данных и добавляются отдельными срезами; `governance-manager` не хранит raw scan report, SBOM, provider payload, runtime logs или секреты.

| Сервис | Что приходит как refs или будущий read-contract | Правило |
|---|---|---|
| `project-catalog` | Project/repository refs, `GetServicesPolicy`, branch rules, release policy, release line, risk profile refs | Проектная policy остаётся у `project-catalog`; governance хранит только risk/gate policy и decisions. |
| `agent-manager` | Flow/run/acceptance refs и role outputs через команды Evaluate/RecordReviewSignal | Flow, run и acceptance остаются у `agent-manager`. |
| `provider-hub` | PR/MR projection, changed file summary, review/comment/check refs, provider write gate ref validation | Provider-native истина остаётся у `provider-hub`. |
| `runtime-manager` | Job status, deploy/postdeploy summary, target environment, blocking runtime signals | Slot/job остаются у `runtime-manager`. |
| `interaction-hub` | Delivery approval request, reminders, escalation, callback result | Доставка и внешние каналы остаются у `interaction-hub`; decision state у governance. |
| `access-manager` | Проверка права на policy manage, gate decision, release decision и high-risk override | Governance не вычисляет права сам. |
| `operations-hub` | Читает `governance.*` события и строит операторские проекции | Operations не принимает доменные решения. |

## Инструменты MCP

Будущие MCP-инструменты должны маршрутизироваться через `platform-mcp-server`:

| Инструмент | Назначение |
|---|---|
| `governance.risk.evaluate` | Запросить оценку риска для transition/PR/release/job. |
| `governance.signal.record_review` | Передать role-driven review signal. |
| `governance.gate.request` | Создать gate request с evidence package. |
| `governance.gate.submit_decision` | Передать human decision, полученное через UI или внешний канал. |
| `governance.gate.cancel` | Отменить открытый gate request без владения delivery callback. |
| `governance.gate.expire` | Зафиксировать истечение открытого gate request. |
| `governance.release.prepare_decision_package` | Собрать пакет релизного решения. |
| `governance.release.submit_decision` | Зафиксировать release go/no-go/hold/rollback/follow-up. |
| `governance.summary.get` | Прочитать безопасную сводку governance по одному selector без переноса state или правил в MCP. |

MCP-инструменты не должны принимать свободный provider diff или секреты. Для provider refs используется `provider-hub`, для delivery callback используется `interaction-hub`.

## Модель ошибок

| Ошибка | Когда возвращается |
|---|---|
| `invalid_argument` | Невалидный target ref, scope, risk class, gate outcome, evidence ref или policy matcher. |
| `permission_denied` | `access-manager` запретил управление policy или принятие решения. |
| `not_found` | Risk profile, assessment, gate, release package или blocking signal не найден. |
| `already_exists` | Дубликат активного slug/profile или повторный final decision для gate. |
| `failed_precondition` | Недостаточно evidence, отсутствует required signal, есть active blocking signal или callback actor не соответствует policy. |
| `aborted` | Конфликт expected version или устаревший assessment/gate state. |
| `unavailable` | Временная ошибка project/provider/runtime/interaction/access/event-log зависимости. |

## События

Все события `governance.*` публикуются через service-local outbox и `platform-event-log`. Payload является safe read-model DTO: содержит только ids, owner-domain refs, status/outcome, reason code, bounded `safe_summary`, actor ref, request/correlation refs, version и envelope timestamp. `idempotency_key` в событии является безопасной correlation-ссылкой: для `command_id` передаётся `command:<uuid>`, для caller-supplied idempotency key — digest, а не исходное значение. Сырые provider payload, diff, prompt/transcript, stdout/stderr, runtime logs, секреты, webhook body и большие отчёты в события не попадают.

| Событие | Когда публикуется |
|---|---|
| `governance.policy.version_activated` | Активирована версия risk profile или gate policy. |
| `governance.risk_assessment.requested` | Запрошена оценка риска. |
| `governance.risk_assessment.completed` | Оценка риска создана или пересчитана. |
| `governance.risk_assessment.changed` | Effective risk class, factors или required gates изменились. |
| `governance.review_signal.recorded` | Записан review/QA/lexical/SRE/security/custom signal. |
| `governance.blocking_signal.recorded` | Зафиксирован блокирующий сигнал. |
| `governance.blocking_signal.resolved` | Блокирующий сигнал закрыт или снят. |
| `governance.gate.requested` | Создан gate request. |
| `governance.gate.resolved` | Gate получил final decision. |
| `governance.gate.cancelled` | Открытый gate request отменён. |
| `governance.gate.expired` | Открытый gate request истёк. |
| `governance.release_decision_package.built` | Собран release evidence package. |
| `governance.release_decision_package.runtime_evidence_recorded` | В существующий release package дозаписаны safe runtime/deploy refs без логов, raw payload и больших details. |
| `governance.release_decision_package.agent_evidence_recorded` | В существующий release package дозаписаны safe agent acceptance/review/runtime refs без prompt/transcript, runtime logs, raw tool output и workspace paths. |
| `governance.release_decision.requested` | Запрошено релизное решение. |
| `governance.release_decision.resolved` | Релизное решение принято. |
| `governance.release_safety_state.changed` | Изменилось состояние release safety-loop. |

### Event-driven/read-model граница

Входящие owner-domain events используются только там, где контракт уже содержит safe outcome/ref metadata:

| Источник | Событие | Условие обработки | Локальный результат |
|---|---|---|---|
| `provider-hub` | `provider.comment.synced` | `review_state=approved` или `changes_requested`, есть provider work item ref и provider comment/comment projection ref | Идемпотентный `RecordReviewSignal` с `reviewer/pass/info` или `reviewer/request_changes/blocking`; повтор того же evidence ref не создаёт второй signal, конфликтующий outcome poisonится как permanent diagnostic. |
| `interaction-hub` | `interaction.request.response_recorded` | `owner_service=governance_manager`, `request_kind=human_gate`, `status=answered`, есть ref на локальный gate request, `response_action=approve` или `reject` | Идемпотентный `SubmitGateDecision` с `approve` или `reject`; повтор того же safe fingerprint возвращает уже записанное решение, конфликтующий fingerprint получает permanent diagnostic без retry storm. |
| `agent-manager` | `agent.acceptance.completed`, `agent.acceptance.failed` | Есть явный `governance_release_decision_package_ref`, acceptance/session refs и terminal status `passed|skipped|failed`, согласованный с типом события | Идемпотентный `RecordReleaseAgentEvidence` для существующего release package; событие без package ref подтверждается без записи, некорректная ссылка или конфликтующий fingerprint получает permanent diagnostic без retry storm. |

Остальные owner-domain events остаются trigger/read-model входами будущих срезов, пока в них нет стабильного typed governance outcome или пока они относятся к другой command boundary.

| Решение или сигнал | Событие для consumers | Основные consumers | Когда нужен gRPC read |
|---|---|---|---|
| Risk assessment lifecycle | `governance.risk_assessment.requested`, `completed`, `changed` | `agent-manager`, `provider-hub`, `platform-mcp-server`, будущие operations projections | Когда consumer нужен authoritative detail по assessment/factors и он имеет право `governance.risk.read`. |
| Review signal refs | `governance.review_signal.recorded` | `agent-manager`, `provider-hub`, release projections | Когда нужно получить список signals по target/assessment или сверить конкретный signal id. |
| Gate request/decision | `governance.gate.requested`, `resolved`, `cancelled`, `expired` | `agent-manager` resume, `interaction-hub` delivery correlation, provider write policy, operations projections | Когда нужно прочитать gate request/decision с evidence refs или проверить final decision перед mutating command. |
| Blocking signals | `governance.blocking_signal.recorded`, `resolved` | release decision consumers, runtime/operations projections | Когда нужен список active/historical blockers по target. |
| Release package/decision/safety-loop | `governance.release_decision_package.built`, `governance.release_decision_package.runtime_evidence_recorded`, `governance.release_decision_package.agent_evidence_recorded`, `governance.release_decision.requested`, `resolved`, `governance.release_safety_state.changed` | `agent-manager`, `runtime-manager`, provider write policy, operations projections | Когда нужен authoritative release package snapshot, decision detail или current safety state. |
| Сводка владельца и персонала | События не заменяют summary lookup | `staff-gateway`, `platform-mcp-server`, будущий `web-console`, operations projections | Когда UI или agent/manager surface нужно показать, что требует решения, какие решения завершены и какие evidence refs связаны с target/release/run без доменной логики в gateway или MCP. |

События служат trigger/read-model основой и не заменяют синхронный gRPC для команд, optimistic concurrency, access checks и точечного authoritative lookup. Соседние сервисы не читают БД `governance-manager`: они реагируют на `platform-event-log` и при необходимости вызывают gRPC reads с текущими правами.

## Состояние реализации

| Область | Статус |
|---|---|
| Доменная документация | Подготовлена как стартовый docs-first срез. |
| gRPC proto `GovernanceManagerService` | Подготовлен как контрактный срез `GOV-1`; покрывает risk profiles/rules, assessments/factors, review signals, gates, release packages/decisions, blocking signals и safety-loop. |
| AsyncAPI `governance.*` | Подготовлен как контрактный срез `GOV-1`; Go-константы событий сгенерированы в `libs/go/platformevents/governance`. |
| Access actions | Добавлены в общий каталог для policy, risk, signal, gate и release операций. |
| Сервисный процесс `governance-manager` | Каркас подготовлен: process, env, health/readiness/metrics, gRPC registration и bounded handlers. |
| Storage, migrations и outbox publisher | MVP-основа готова: PostgreSQL repository, service-local outbox и handlers для поддержанных storage-операций. Review signals имеют локальную ref-level дедупликацию по normalized owner-domain evidence refs. |
| Risk classifier и policy evaluator | Готовы для локальных risk profiles/rules, safe summaries/refs, идемпотентного replay, optimistic concurrency и safe outbox events. |
| Release decision lifecycle и safety-loop | Готовы для release package build/read/list, decision request/submit/read/list, blocking signals и текущего safety-loop state на safe refs/summaries. |
| Release integration refs | Поддержаны для release decision package: safe domain/kind/ref/status/summary/digest/timestamp/version/error_code, canonical order по `domain/kind/ref`, reject конфликтующих дублей, локальная проверка governance refs и отсутствие raw payload/logs/secrets. |
| Runtime/deploy evidence refs | `RecordReleaseRuntimeEvidence` дозаписывает `runtime_refs`, bounded `evidence_refs` и `integration_refs` домена `runtime` в существующий release package с expected version, replay и safe `governance.release_decision_package.runtime_evidence_recorded` event. |
| Agent evidence refs | `RecordReleaseAgentEvidence` дозаписывает `agent_context`, bounded `evidence_refs` и `integration_refs` доменов `agent`, `runtime` и `governance` в существующий release package с expected version, replay и safe `governance.release_decision_package.agent_evidence_recorded` event. |
| Сводка чтения governance | `GetGovernanceSummary` готовит безопасную модель чтения для владельца, персонала и manager/slot-агентов по target/project/release/package/integration ref: live `status` rollup, pending/completed decisions, risk class, gate/release outcomes, linked provider/agent/runtime evidence refs и partial diagnostics без raw payload; новый `status` доступен через gRPC и `platform-mcp-server`, а HTTP DTO в `staff-gateway` обновляется отдельным срезом. |
| Event-driven/read-model основа | `governance.*` payload расширен safe metadata/refs: actor, request id, idempotency correlation, target/source refs, ограниченный summary, interaction/agent/runtime refs и policy/decision refs. Соседние сервисы могут строить read models через `platform-event-log` без чтения БД governance. |
| Потребитель provider review signal | Готов для `provider.comment.synced`: approved/changes_requested review refs превращаются в локальный review signal через `libs/go/eventconsumer`, без чтения БД/API `provider-hub` и без копирования provider-native state. |
| Потребитель interaction gate decision | Готов для `interaction.request.response_recorded`: answered Human gate response для `owner_service=governance_manager` и локального gate ref превращается в `SubmitGateDecision` по safe refs/digest/outcome без чтения БД/API `interaction-hub` и без копирования response text/callback body. |
| Потребитель agent acceptance evidence | Готов для `agent.acceptance.completed` и `agent.acceptance.failed`: события с явным `governance_release_decision_package_ref` дозаписывают safe agent acceptance/runtime job evidence в существующий release package через `RecordReleaseAgentEvidence`; события без package ref подтверждаются без записи. |
| Интеграции с project/agent/provider/runtime/interaction | Зафиксированы в refs и границах контрактов; межсервисные read-клиенты, delivery callbacks, provider write и deploy orchestration остаются отдельными срезами. |

## Совместимость

- `v1` контракт покрывает согласованный минимум до сервисной реализации.
- Risk/gate/release refs проектируются provider-neutral, чтобы GitLab не требовал смены модели.
- События должны быть безопасны для outbox/inbox на PostgreSQL и будущего брокера.
- Gate/release decisions являются audit-critical и не удаляются без отдельной retention policy.
- Ошибки, события и evidence package не содержат сырые provider payload, значения секретов, полный diff, полные логи или большие вложения; для них используются typed refs, digest и ограниченный summary.

## Апрув

- request_id: `owner-2026-05-22-risk-governance-kickoff`
- Решение: pending
- Комментарий: API-обзор синхронизирован с реализованными storage, evaluator, gate и release lifecycle возможностями; межсервисные интеграции остаются отдельным этапом.
