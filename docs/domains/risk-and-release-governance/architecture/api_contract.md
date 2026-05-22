---
doc_id: API-CK8S-RISK-GOVERNANCE-0001
type: api-contract
title: kodex — API-обзор governance-manager
status: active
owner_role: SA
created_at: 2026-05-22
updated_at: 2026-05-22
related_issues: [322]
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
- Версионирование: транспортное пространство имён будет `kodex.governance.v1` после согласования документации.
- Основные операции: risk profiles, risk assessments, review signals, gate requests/decisions, release decision packages, release decisions и safety-loop signals.

## Спецификации

- gRPC proto: не создаётся в этом docs-first срезе; плановый путь `proto/kodex/governance/v1/governance_manager.proto`.
- AsyncAPI: не создаётся в этом docs-first срезе; плановый путь `specs/asyncapi/governance-manager.v1.yaml`.
- MCP-инструменты: будут публиковаться через `platform-mcp-server`, не напрямую из доменного сервиса.
- Внешний HTTP для будущей консоли: через профильный gateway, не напрямую из `governance-manager`.

Этот документ является обзором целевого API. После создания proto/AsyncAPI машинные спецификации станут источником правды транспорта, а документ должен обновляться синхронно с изменением транспортной спецификации.

## Операции

| Операция | Вид | Доступ | Идемпотентность | Примечание |
|---|---|---|---|---|
| `CreateRiskProfile` | gRPC command | `governance.policy.manage` | `CommandMeta.command_id` | Создаёт профиль риска для scope. |
| `CreateRiskProfileVersion` | gRPC command | `governance.policy.manage` | `command_id` | Создаёт версию правил риска и gate policy. |
| `ActivateRiskProfileVersion` | gRPC command | `governance.policy.manage` | `command_id` + expected version | Активирует версию для новых evaluations. |
| `GetRiskProfile` | gRPC query | `governance.policy.read` | нет | Читает профиль и активную версию. |
| `ListRiskProfiles` | gRPC query | `governance.policy.read` | нет | Читает профили по scope/status. |
| `EvaluateRisk` | gRPC command | `governance.risk.evaluate` | `command_id` | Создаёт или обновляет assessment для transition, PR/MR, release candidate, job или policy change. |
| `ReevaluateRisk` | gRPC command | `governance.risk.evaluate` | `command_id` + expected version | Пересчитывает assessment после новых signals или policy version. |
| `GetRiskAssessment` | gRPC query | `governance.risk.read` | нет | Читает assessment, factors и required gates. |
| `ListRiskAssessments` | gRPC query | `governance.risk.read` | нет | Читает assessments по project/repository/target/risk class/status. |
| `RecordReviewSignal` | gRPC command | `governance.signal.record` | `command_id` | Записывает signal от reviewer, QA, lexical gatekeeper, SRE, security или custom role. |
| `ListReviewSignals` | gRPC query | `governance.signal.read` | нет | Читает signals по target или assessment. |
| `RequestGate` | gRPC command | `governance.gate.request` | `command_id` | Создаёт gate request, evidence package и delivery request в `interaction-hub`. |
| `SubmitGateDecision` | gRPC command | `governance.gate.decide` | `command_id` + expected version | Фиксирует решение из UI/provider/external callback после проверки actor policy. |
| `GetGateRequest` | gRPC query | `governance.gate.read` | нет | Читает gate request, evidence и decision status. |
| `ListGateRequests` | gRPC query | `governance.gate.read` | нет | Читает ожидающие, resolved или просроченные gates. |
| `BuildReleaseDecisionPackage` | gRPC command | `governance.release.prepare` | `command_id` | Собирает release evidence из project/provider/runtime/agent refs. |
| `RequestReleaseDecision` | gRPC command | `governance.release.request` | `command_id` | Запрашивает release gate или автоматическое decision по policy. |
| `SubmitReleaseDecision` | gRPC command | `governance.release.decide` | `command_id` + expected version | Фиксирует go/no-go/hold/rollback/follow-up decision. |
| `RecordBlockingSignal` | gRPC command | `governance.signal.record` | `command_id` | Фиксирует blocking signal от acceptance, runtime, provider, interaction, monitoring или человека. |
| `ResolveBlockingSignal` | gRPC command | `governance.signal.resolve` | `command_id` + expected version | Закрывает blocking signal с reason. |
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
| `governance.release_decision.requested` | Запрошено релизное решение. |
| `governance.release_decision.resolved` | Релизное решение принято. |
| `governance.release_safety_state.changed` | Изменилось состояние release safety-loop. |

## Состояние реализации

| Область | Статус |
|---|---|
| Доменная документация | Создаётся стартовым docs-first срезом. |
| gRPC proto | Не создаётся до согласования документации. |
| AsyncAPI `governance.*` | Не создаётся до согласования документации. |
| Сервисный процесс `governance-manager` | Не создаётся до контрактного среза. |
| Интеграции с project/agent/provider/runtime/interaction | Описаны как целевые границы; реализация идёт отдельными срезами. |

## Совместимость

- `v1` контракт должен покрыть согласованный минимум до сервисной реализации.
- Risk/gate/release refs проектируются provider-neutral, чтобы GitLab не требовал смены модели.
- События должны быть безопасны для outbox/inbox на PostgreSQL и будущего брокера.
- Gate/release decisions являются audit-critical и не удаляются без отдельной retention policy.

## Апрув

- request_id: `owner-2026-05-22-risk-governance-kickoff`
- Решение: pending
- Комментарий: API-обзор описывает будущий контракт без создания proto/AsyncAPI в стартовом документационном срезе.
