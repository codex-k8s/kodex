---
doc_id: MON-CK8S-INTERACTION-HUB-0001
type: monitoring
title: "interaction-hub — Monitoring & Observability"
status: active
owner_role: SRE
created_at: 2026-05-27
updated_at: 2026-05-27
related_issues: [894]
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-27-interaction-hub-ops-contour"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-27
---

# Monitoring & Observability: interaction-hub

## TL;DR

- Health: `/health/livez`, `/health/readyz`.
- Metrics: `/metrics`.
- Critical dependencies: service PostgreSQL DB and `platform-event-log` PostgreSQL DB.
- Critical lifecycle: request/response commands, delivery/callback state transitions and service-local outbox dispatch.

## Источники данных

| Источник | Что проверять |
|---|---|
| Kubernetes Deployment | Replicas, rollout status, restart count, readiness. |
| Migration Job | Завершение `interaction-hub-migrations` перед rollout. |
| HTTP health endpoints | `livez` для процесса, `readyz` для service DB и event-log DB. |
| Prometheus metrics | HTTP health, gRPC server metrics, outbox dispatcher metrics, Go runtime metrics. |
| Pod logs | Только bounded diagnostics: operation, status, error class/code, request id/correlation id. |

## Golden signals

| Signal | Метрики или проверка | Реакция |
|---|---|---|
| Latency | gRPC latency по `InteractionHubService`, outbox publish duration | Проверить PostgreSQL latency, lock contention и размер pending outbox. |
| Traffic | gRPC request count по method, outbox publish count | Сверить с ожидаемым backend scenario и callback volume. |
| Errors | gRPC status codes, outbox publish failures, migration failures | Проверить bounded error code, idempotency conflicts и event-log DB readiness. |
| Saturation | DB pool usage, goroutines, pod CPU/memory, outbox claim backlog | Увеличить replicas/resources или DB pool только после проверки slow queries и event-log состояния. |

## Доменные индикаторы

- Количество active/pending interaction requests по kind/status.
- Количество terminal responses по action.
- Количество callback diagnostics с `processing_status=rejected` или safe no-op.
- Количество delivery attempts по status `planned`, `accepted`, `delivered`, `failed`, `expired`.
- Размер service-local outbox backlog и число permanently failed events.

## Логи

Логи не должны содержать raw message body, raw callback payload, full headers, tokens, prompt/transcript, stdout/stderr, DSN или secret values. Для корреляции использовать request id, command id, correlation id, aggregate id, event type, status и bounded error code.

## Health checks

- `livez` подтверждает, что процесс и HTTP server отвечают.
- `readyz` подтверждает готовность доменного service, service DB и event-log DB, когда включён `postgres-event-log` outbox publisher.
- Smoke-проверка `scripts/smoke-interaction-hub.sh` применяет foundation, миграции, deployment и проверяет `/health/readyz`.

## Alerts

| Условие | Severity | Действие |
|---|---|---|
| `interaction-hub` replicas not ready дольше rollout timeout | page | Выполнить runbook диагностики deployment/init container/DB. |
| Migration job failed | page | Проверить goose output и совместимость схемы. |
| Readyz failed при livez OK | page | Проверить DB/event-log DSN и доступность PostgreSQL. |
| Outbox backlog растёт дольше операционного окна | ticket/page по скорости роста | Проверить event-log DB, publisher kind, lock TTL и failure diagnostics. |
| Rejected callback diagnostics резко растут | ticket | Проверить gateway/channel package contract и idempotency/correlation refs. |

## Границы наблюдаемости

`interaction-hub` показывает только собственные request, response, delivery, callback и outbox diagnostics. Cross-domain operator inbox, provider write status, run/session state, runtime jobs и package installation status агрегируются владельцами соседних доменов.

## Апрув

- request_id: `owner-2026-05-27-interaction-hub-ops-contour`
- Решение: approved
- Комментарий: мониторинг `interaction-hub` согласован для первого backend deploy и безопасной диагностики без raw payload и secret values.
