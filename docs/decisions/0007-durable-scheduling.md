---
id: ADR-MC-007
title: Durable scheduling в PostgreSQL
type: decision
status: proposed
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# ADR-MC-007. Durable scheduling в PostgreSQL

## Решение

AutomationSchedule и next-run state хранятся в PostgreSQL. Scheduler создает unique occurrence и queue job transactionally. Kubernetes CronJob и in-memory periodic scheduler не являются business source of truth.

River OSS рассматривается как execution queue для transactional jobs/retries, а cron parser — как готовая библиотека. Durable definitions, misfire/concurrency policies и occurrence history принадлежат MatterCodex.

## Последствия

- Несколько replicas работают без duplicate occurrence.
- Headless scheduled session является полноценным runtime source.
- Не появляется отдельный Redis/Temporal dependency на первом этапе.
- Temporal может быть рассмотрен позже для сложных deterministic workflows.
