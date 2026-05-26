---
doc_id: API-CK8S-RISK-GOVERNANCE-0001
type: api-contract
title: kodex — API-обзор governance-manager
status: active
owner_role: SA
created_at: 2026-05-22
updated_at: 2026-05-26
related_issues: [322, 769, 790, 815, 827]
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
- MCP-инструменты: будут публиковаться через `platform-mcp-server`, не напрямую из доменного сервиса.
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
| `RecordReviewSignal` | gRPC command | `governance.signal.record` | `command_id` | Записывает signal от reviewer, QA, lexical gatekeeper, SRE, security или custom role. |
| `ListReviewSignals` | gRPC query | `governance.signal.read` | нет | Читает signals по target или assessment. |
| `RequestGate` | gRPC command | `governance.gate.request` | `command_id` | Создаёт governance gate request и evidence package; delivery request/ref остаётся у `interaction-hub`. |
| `SubmitGateDecision` | gRPC command | `governance.gate.decide` | `command_id` + expected version | Фиксирует решение из UI/provider/external callback после проверки actor policy. |
| `CancelGate` | gRPC command | `governance.gate.decide` | `command_id` + expected version | Переводит открытый gate request в `cancelled`; доставка и callback остаются у `interaction-hub`. |
| `ExpireGate` | gRPC command | `governance.gate.decide` | `command_id` + expected version | Переводит открытый gate request в `expired` после timeout policy или delivery expiry. |
| `GetGateDecision` | gRPC query | `governance.gate.read` | `gate_request_id` | Читает одно final gate decision после fail-closed проверки доступа по gate request. |
| `ListGateDecisions` | gRPC query | `governance.gate.read` | gate request или target | Читает final gate decisions по gate request или target; outcome используется только как уточняющий фильтр. |
| `GetGateRequest` | gRPC query | `governance.gate.read` | нет | Читает gate request, evidence и decision status. |
| `ListGateRequests` | gRPC query | `governance.gate.read` или `governance.risk.read` | target или risk assessment | Читает ожидающие, resolved или просроченные gates по target или assessment; status используется только как уточняющий фильтр. |
| `BuildReleaseDecisionPackage` | gRPC command | `governance.release.prepare` | `command_id` | Собирает release evidence из project/provider/runtime/agent refs. |
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

## Интеграции с другими сервисами

| Сервис | Вызовы из `governance-manager` | Правило |
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
| `governance.release_decision.requested` | Запрошено релизное решение. |
| `governance.release_decision.resolved` | Релизное решение принято. |
| `governance.release_safety_state.changed` | Изменилось состояние release safety-loop. |

## Состояние реализации

| Область | Статус |
|---|---|
| Доменная документация | Подготовлена как стартовый docs-first срез. |
| gRPC proto `GovernanceManagerService` | Подготовлен как контрактный срез `GOV-1`; покрывает risk profiles/rules, assessments/factors, review signals, gates, release packages/decisions, blocking signals и safety-loop. |
| AsyncAPI `governance.*` | Подготовлен как контрактный срез `GOV-1`; Go-константы событий сгенерированы в `libs/go/platformevents/governance`. |
| Access actions | Добавлены в общий каталог для policy, risk, signal, gate и release операций. |
| Сервисный процесс `governance-manager` | Каркас подготовлен: process, env, health/readiness/metrics, gRPC registration и bounded handlers. |
| Storage, migrations и outbox publisher | MVP-основа готова: PostgreSQL repository, service-local outbox и handlers для поддержанных storage-операций. |
| Risk classifier и policy evaluator | Готовы для локальных risk profiles/rules, safe summaries/refs, идемпотентного replay, optimistic concurrency и safe outbox events. |
| Release decision engine и safety-loop | Не реализованы; остаются следующими срезами после risk evaluator. |
| Интеграции с project/agent/provider/runtime/interaction | Зафиксированы в refs и границах контрактов; реализация идёт отдельными срезами. |

## Совместимость

- `v1` контракт покрывает согласованный минимум до сервисной реализации.
- Risk/gate/release refs проектируются provider-neutral, чтобы GitLab не требовал смены модели.
- События должны быть безопасны для outbox/inbox на PostgreSQL и будущего брокера.
- Gate/release decisions являются audit-critical и не удаляются без отдельной retention policy.
- Ошибки, события и evidence package не содержат сырые provider payload, значения секретов, полный diff, полные логи или большие вложения; для них используются typed refs, digest и bounded summary.

## Апрув

- request_id: `owner-2026-05-22-risk-governance-kickoff`
- Решение: pending
- Комментарий: API-обзор синхронизирован с контрактным срезом GOV-1; сервисная реализация и интеграции остаются следующими срезами.
