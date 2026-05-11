---
doc_id: MON-CK8S-RUNTIME-MANAGER-0001
type: monitoring
title: "runtime-manager — наблюдаемость"
status: active
owner_role: SRE
created_at: 2026-05-08
updated_at: 2026-05-08
related_issues: [661, 662]
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-07-runtime-manager-kickoff"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-07
---

# Наблюдаемость: runtime-manager

## TL;DR
- Дашборды: будут строиться поверх HTTP readiness, gRPC метрик, PostgreSQL и outbox-доставки.
- Метрики: активные слоты, задания, длительность подготовки workspace, ошибки cleanup, outbox backlog.
- Логи: только безопасная диагностика с `request_id`, `slot_id`, `job_id`, `workspace_materialization_id`.
- Алерты: недоступность readiness, рост failed jobs, застрявшие leases, backlog outbox, cleanup failures.

## Источники данных

- HTTP health: `/health/livez`, `/health/readyz`.
- gRPC server metrics: общий runtime из `libs/go/grpcserver`.
- PostgreSQL: БД `kodex_runtime_manager` и общая БД `kodex_platform_event_log`.
- Kubernetes: deployment, migration job, pod status и events.
- Логи приложения: structured logs без секретов и PII.

## Дашборды

| Название | Ссылка | Для чего | Owner |
|---|---|---|---|
| Runtime manager overview | TBD | Общий статус сервиса, readiness, gRPC, БД, outbox. | SRE |
| Runtime jobs | TBD | Pending/running/failed jobs, длительность шагов, short log tail signals. | SRE |
| Slots and workspaces | TBD | Активные слоты, leases, materialization duration, failed materialization. | SRE |

## Метрики

### Golden signals

- Latency: длительность gRPC-команд и чтений.
- Traffic: количество gRPC-запросов по методам.
- Errors: gRPC-коды, ошибки БД, ошибки outbox.
- Saturation: `MaxInFlight`, active connections PostgreSQL, размер очереди outbox.

### Runtime-сигналы

- Количество слотов по статусам.
- Количество заданий по статусам и типам.
- Длительность workspace materialization.
- Количество `runtime.job.failed` и `runtime.workspace.materialization_failed`.
- Количество cleanup failures.
- Возраст самого старого неопубликованного outbox-события.

## Логи

- Логи должны содержать безопасные идентификаторы: `request_id`, `actor_type`, `actor_id`, `slot_id`, `job_id`, `step_key`, `fleet_scope_id`, `cluster_id`.
- Полные runtime-логи job не пишутся в PostgreSQL; в БД хранится только `short_log_tail` и `full_log_ref`.
- Секреты, DSN, токены, email и сырые payload внешних провайдеров в логи не попадают.

## Проверки и рутинные health checks

- Liveness: процесс отвечает на `/health/livez`.
- Readiness: процесс видит БД `runtime-manager` и, при включённой outbox-доставке, БД `platform-event-log`.
- gRPC smoke: вызов `RuntimeManagerService/GetSlot` должен давать application-level статус, а не сетевую ошибку.

## Алерты

- `runtime-manager` readiness недоступен дольше установленного окна.
- `runtime-manager` migration job завершился ошибкой.
- Outbox backlog растёт или самое старое событие старше допустимого порога.
- Количество failed jobs превысило baseline.
- Слоты или job leases застряли после истечения TTL.
- Cleanup failures не устранены за допустимое окно.

## Открытые вопросы

- Конкретные Prometheus recording rules и alert rules будут закреплены после появления штатного observability stack.
- Cleanup/prewarm dashboards уточняются после реализации RTM-7.

## Апрув
- request_id: `owner-2026-05-07-runtime-manager-kickoff`
- Решение: approved
- Комментарий: monitoring-документ входит в эксплуатационный контур RTM-6.
