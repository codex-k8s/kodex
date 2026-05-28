---
doc_id: MON-CK8S-FLEET-MANAGER-0001
type: monitoring
title: "fleet-manager — наблюдаемость"
status: active
owner_role: SRE
created_at: 2026-05-13
updated_at: 2026-05-13
related_issues: [738]
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-11-fleet-manager-kickoff"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-11
---

# Наблюдаемость: fleet-manager

## TL;DR

- Дашборды: readiness, gRPC, PostgreSQL, outbox, registry, health snapshots и placement decisions.
- Метрики: ошибки доступа, деградация cluster health, backlog outbox, latency `ResolvePlacement`, результаты connectivity checks.
- Логи: только безопасные идентификаторы `request_id`, `fleet_scope_id`, `server_id`, `cluster_id`, `placement_decision_id`.
- Алерты: недоступность readiness, падение migration job, degraded/default cluster, рост rejected placement и outbox backlog.

## Источники данных

- HTTP health: `/health/livez`, `/health/readyz`.
- HTTP metrics: `/metrics`.
- gRPC server metrics: общий runtime из `libs/go/grpcserver`.
- PostgreSQL: БД `kodex_fleet_manager` и общая БД `kodex_platform_event_log`.
- Kubernetes: deployment, migration job, pod status и events.
- Логи приложения: structured logs без секретов, kubeconfig, DSN, токенов и PII.

## Дашборды

| Название | Ссылка | Для чего | Owner |
|---|---|---|---|
| Fleet manager overview | TBD | Общий статус сервиса, readiness, gRPC, БД, outbox. | SRE |
| Fleet registry | TBD | Scope/server/cluster counts по статусам и default-пути. | SRE |
| Fleet health | TBD | Последние health snapshots, degraded/offline clusters, latency checks. | SRE |
| Fleet placement | TBD | `ResolvePlacement` latency, selected/rejected decisions, причины отказов. | SRE |

## Golden signals

- Latency: длительность gRPC-команд, чтений, `ResolvePlacement` и connectivity checks.
- Traffic: количество gRPC-запросов по методам.
- Errors: gRPC-коды, ошибки БД, ошибки access-manager, ошибки secretresolver, ошибки outbox.
- Saturation: `MaxInFlight`, active PostgreSQL connections, размер outbox backlog, конкуренция за placement rules.

## Fleet-сигналы

- Количество fleet scope по статусам и типам.
- Количество server по статусам, регионам и capacity class.
- Количество Kubernetes cluster по статусам и health.
- Возраст последнего health snapshot для каждого active cluster.
- Количество `fleet.health.degraded` и `fleet.health.checked`.
- Количество `fleet.placement.resolved` и `fleet.placement.rejected`.
- Возраст самого старого неопубликованного outbox-события.

## Логи

Логи должны содержать только безопасные идентификаторы:

- `request_id`, `actor_type`, `actor_id`;
- `fleet_scope_id`, `server_id`, `cluster_id`;
- `health_snapshot_id`, `placement_rule_id`, `placement_decision_id`;
- gRPC method и application status.

В логи не попадают:

- kubeconfig или service account token;
- DSN, gRPC tokens, Vault token;
- реальные приватные endpoint, если они относятся к целевому серверу или секретному контуру;
- сырые Kubernetes objects, events и logs.

## Проверки и рутинные health checks

- Liveness: процесс отвечает на `/health/livez`.
- Readiness: процесс видит БД `fleet-manager` и, при включённой outbox-доставке, БД `platform-event-log`.
- gRPC integration check: `FleetManagerService/ListFleetScopes` должен давать application-level статус, а не сетевую ошибку.
- Connectivity checks: latest snapshot для active/default cluster не должен быть старше допустимого окна, если кластер используется для placement.

## Алерты

- `fleet-manager` readiness недоступен дольше установленного окна.
- `fleet-manager` migration job завершился ошибкой.
- Outbox backlog растёт или самое старое событие старше допустимого порога.
- Default cluster в `platform-default` scope стал degraded/offline.
- Доля rejected placement превышает baseline.
- Health snapshot для active cluster устарел.
- Ошибки secretresolver при connectivity checks повторяются.
- `access-manager` недоступен для fleet-команд дольше допустимого окна.

## Открытые вопросы

- Конкретные Prometheus recording rules и alert rules будут закреплены после появления штатного observability stack.
- Capacity/rebalancing dashboards относятся к post-MVP автоматизации fleet и не входят в FLEET-6.

## Апрув

- request_id: `owner-2026-05-11-fleet-manager-kickoff`
- Решение: approved
- Комментарий: monitoring-документ входит в эксплуатационный контур FLEET-6.
