---
id: DOM-MC-011
title: Operations & Observability
type: domain
status: proposed
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# Operations & Observability

## Назначение

Предоставляет operational read models, telemetry correlation, capacity state, incidents, backup evidence и безопасную диагностику.

Не принимает бизнес-решения за Runtime/Integrations и не использует Grafana как source of truth для scheduler.

## Signals

Metrics:

- queue depth/age;
- turn latency/duration/outcome/retries;
- session pods by state;
- pending/OOM/evictions/capacity;
- provider auth/limits freshness;
- integration/approval latency and failures;
- artifact ingestion/delivery/scan;
- image build/cache/scan;
- schedule delay/misfire/duplicate prevention;
- backup age/restore drill status.

Logs имеют organization/workspace/session/turn/process/correlation IDs и не содержат secret/raw sensitive content.

Traces связывают Mattermost event, command, queue, pod, provider, MCP invocation и delivery.

## Operational status

Control Center показывает user-safe и operator detail уровни. Mattermost error card содержит краткую причину и next action; внутренний stacktrace остается в observability backend.

## Capacity control

Runtime использует Kubernetes API/metrics для текущего admission, а Prometheus — для трендов, alerts и планирования. Idle eviction не затрагивает active/queued sessions.

## Acceptance

- Один correlation ID проходит end-to-end.
- Alert содержит runbook link.
- Owner видит queue/capacity/account/backup status без kubectl.
- OOM/pending/transient provider error классифицируются раздельно.
- Telemetry pipeline сам наблюдаем и ограничивает memory/backpressure.
