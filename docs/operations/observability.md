---
id: OPS-MC-004
title: Наблюдаемость
type: operations
status: proposed
owner: sre
version: 0.1.0
updated: 2026-07-16
---

# Наблюдаемость

## Стек

- OpenTelemetry SDK/Collector для traces, metrics и log correlation;
- Prometheus-compatible metrics storage;
- Grafana dashboards/alerts;
- централизованный log backend;
- Kubernetes metrics/events для runtime diagnostics.

## Correlation

Минимальные labels/attributes:

- service/version/environment;
- organization/workspace;
- agent/session/turn;
- process/child/schedule occurrence;
- integration/tool/approval;
- correlation/causation IDs.

Высококардинальные IDs не добавляются бездумно в Prometheus labels; они доступны в traces/logs.

## Dashboards

- platform API/DB/outbox;
- Mattermost ingress/delivery;
- turns/sessions/queue/retries;
- Kubernetes capacity/pending/OOM/eviction;
- AI accounts auth/limits freshness;
- integrations/approvals;
- artifacts/scan/delivery;
- schedules/misfires;
- image builds/scans/cache;
- backup/restore evidence.

## Alerts

Alert имеет severity, symptom, impact, correlation/dashboard и runbook link. Нельзя алертить на каждую ожидаемую provider retry. Обязательны alerts на потерю leadership/queue progress, backup age, persistent delivery failure, DB/S3 unavailability и capacity exhaustion.

## Privacy

Prompts, file contents, tokens, auth JSON и secret arguments не экспортируются. User content logging выключен по умолчанию; диагностическое включение ограничено scope/TTL/audit.
