---
doc_id: MON-CK8S-GOVERNANCE-MANAGER-0001
type: monitoring
title: "governance-manager — наблюдаемость"
status: active
owner_role: SRE
created_at: 2026-05-27
updated_at: 2026-05-27
related_issues: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-27-governance-manager-ops"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-27
---

# Наблюдаемость: governance-manager

## TL;DR

- Дашборды: readiness, gRPC, PostgreSQL, outbox, risk assessments, review signals, gates, release decisions и safety-loop.
- Метрики: ошибки доступа, ошибки БД, outbox backlog, latency gRPC-команд, число pending gates, blocked release decisions и safety-loop hold/rollback.
- Логи: только безопасные идентификаторы `request_id`, `actor_ref`, `target_ref`, `risk_assessment_id`, `gate_request_id`, `release_decision_package_id`, `release_decision_id`.
- Алерты: недоступность readiness, падение migration job, outbox backlog, рост expired gates, повторные failed/blocked release decisions и отсутствие platform-event-log.

## Источники данных

- HTTP health: `/health/livez`, `/health/readyz`.
- HTTP metrics: `/metrics`.
- gRPC server metrics: общий runtime из `libs/go/grpcserver`.
- PostgreSQL: БД `kodex_governance_manager` и общая БД `kodex_platform_event_log`.
- Kubernetes: deployment, migration job, pod status и events.
- Логи приложения: structured logs без секретов, raw provider payload, prompt/transcript, stdout/stderr, kubeconfig, DSN, токенов, email и приватных endpoint.

## Дашборды

| Название | Ссылка | Для чего | Owner |
|---|---|---|---|
| Governance manager overview | TBD | Общий статус сервиса, readiness, gRPC, БД и outbox. | SRE |
| Governance risk and signals | TBD | Risk assessments, factors, review signals, blocking signals и required gates. | SRE |
| Governance gates | TBD | Pending/resolved/expired/cancelled gates, latency decision и конфликт expected version. | SRE |
| Governance releases | TBD | Release packages, decisions, safety-loop states, hold/rollback/follow-up outcomes. | SRE |

## Golden signals

- Latency: длительность gRPC-команд, PostgreSQL-запросов, access-manager checks и outbox publication.
- Traffic: количество gRPC-запросов, созданных assessments, review signals, gate requests, release packages и decisions.
- Errors: gRPC-коды, ошибки БД, ошибки `access-manager`, ошибки валидации refs/summaries и ошибки outbox.
- Saturation: `MaxInFlight`, active PostgreSQL connections, размер outbox backlog, число pending gates и число release decisions в non-terminal состояниях.

## Доменный мониторинг

- Количество `RiskAssessment` по `initial_risk_class`, `effective_risk_class` и статусам.
- Количество `ReviewSignal` по outcome/severity и target type.
- Количество `GateRequest` по статусам `requested`, `delivering`, `awaiting_decision`, `resolved`, `expired`, `cancelled`.
- Возраст самого старого pending/awaiting gate.
- Количество `ReleaseDecisionPackage` по статусам и project/release candidate scope.
- Количество `ReleaseDecision` по outcome/status: approved, blocked, waiting, failed, cancelled или текущие enum контракта.
- Количество активных `BlockingSignal` и скорость их закрытия.
- Распределение `ReleaseSafetyState`: stable, hold, rollback, follow-up required и промежуточные состояния.
- Частота конфликтов idempotency/source fingerprint и expected version.

## Логи

Логи должны содержать только безопасные идентификаторы:

- `request_id`, `trace_id`, `actor_type`, `actor_ref`;
- `target_type`, `target_ref`, `project_ref`, `repository_ref`;
- `risk_assessment_id`, `review_signal_id`, `gate_request_id`, `gate_decision_id`;
- `release_decision_package_id`, `release_decision_id`, `blocking_signal_id`;
- короткий `error_code`, gRPC code и безопасный status summary.

В логи не попадают:

- gRPC token, DSN, webhook secret, Vault token и любые значения секретов;
- raw provider payload, raw diff, full changed file list и provider API response;
- prompt, transcript, stdout/stderr, runtime logs, kubeconfig и workspace paths;
- email, приватные домены, адреса серверов и PII из локального bootstrap-профиля.

## Проверки и рутинные health checks

- Liveness: процесс отвечает на `/health/livez`.
- Readiness: процесс видит БД `governance-manager` и, при включённой outbox-доставке, БД `platform-event-log`.
- Smoke: `scripts/smoke-governance-manager.sh` подтверждает migration job, deployment rollout и `/health/readyz`.
- Access boundary: команды управления risk/gate/release decision должны получать application-level отказ, а не transport failure, если actor не имеет права.
- Outbox: локальная очередь не должна расти дольше допустимого окна.

## Алерты

- `governance-manager` readiness недоступен дольше установленного окна.
- `governance-manager` migration job завершился ошибкой.
- Outbox backlog растёт или самое старое событие старше допустимого порога.
- `access-manager` недоступен для governance access checks.
- Количество expired gates растёт быстрее expected baseline.
- Pending/awaiting gate старше policy timeout без transition в `expired` или `resolved`.
- Повторные release decisions завершаются `blocked`/`failed` по одной причине для одного scope.
- Safety-loop states `hold`, `rollback` или `follow_up_required` держатся дольше согласованного окна.
- Частота конфликтов source fingerprint или expected version резко выросла.

## Открытые вопросы

- Конкретные Prometheus recording rules и alert rules будут закреплены после появления штатного observability stack.
- Operator projections для cross-domain dashboards появятся после отдельного integration/operations среза.
- Service-client health для `project-catalog`, `provider-hub`, `agent-manager`, `runtime-manager` и `interaction-hub` не включается до появления соответствующих read-client интеграций.

## Апрув

- request_id: `owner-2026-05-27-governance-manager-ops`
- Решение: approved
- Комментарий: monitoring-документ фиксирует наблюдаемость `governance-manager` для первого backend deploy.
