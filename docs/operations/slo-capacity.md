---
id: OPS-MC-003
title: SLO и управление ресурсами
type: operations
status: proposed
owner: sre
version: 0.1.0
updated: 2026-07-16
---

# SLO и управление ресурсами

## Начальные service objectives

Значения являются стартовыми и уточняются после измерений:

- control/interaction API production availability: 99.9% за месяц;
- подтверждение принятого Mattermost сообщения: p95 до 2 секунд без учета запуска агента;
- durable queue: отсутствие потери accepted turn при одиночном restart/failure stateless service;
- duplicate scheduled occurrence: 0 для одного `(schedule_id, scheduled_for)`;
- backup RPO: не более 15 минут для PostgreSQL WAL;
- platform restore RTO: до 4 часов для документированного supported topology;
- stale running session repair: обнаружение до 2 reconciliation intervals.

AI provider latency/availability измеряется отдельно и не включается в platform availability, но ошибка должна быть классифицирована и показана пользователю.

## Resource classes

Agent/role выбирает versioned ResourceClass с CPU/memory/ephemeral storage requests, optional memory limit и workload hints. Произвольные значения доступны только administrator.

## Admission

Перед созданием pod runtime controller проверяет:

- allocatable/node pressure;
- namespace quotas;
- pending workloads;
- resource class requests;
- максимальную параллельность organization/account;
- доступные idle pods для eviction.

При нехватке capacity turn остается queued. Permanent failure создается только после policy timeout либо неустранимой validation error.

## Warm pods

- максимум один pod на session;
- default TTL четыре часа;
- LRU eviction только idle sessions без queued/running turn;
- session archive/PVC metadata не удаляются при eviction;
- warm pod count и reserved resources наблюдаемы.

## Limits

CPU limits для agent/build workloads не обязательны и применяются только с измеренной причиной. Memory requests/limits и node headroom должны предотвращать host OOM. Production values определяются load tests, а не размером текущего сервера.
