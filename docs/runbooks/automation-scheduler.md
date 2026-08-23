---
id: RUN-MC-016
title: Диагностика automation-scheduler
type: runbook
status: approved
owner: sre
version: 2.0.0
updated: 2026-08-23
---

# Диагностика automation-scheduler

Scheduler является worker, а не владельцем Schedule. Control-plane хранит
target, timezone, preset/cron, input, session/notification policies,
occurrences, attempts и lifecycle.

## Цикл

1. Claim exact due occurrence по generated RPC.
2. Получить occurrence/version/generation/fence/immutable input digest.
3. Попросить control-plane создать Run source `SCHEDULE`.
4. Завершить цикл: terminal RuntimeWork transaction сама обновит occurrence и
   Schedule вместе с авторитетным Run graph.

Schedule запускает Agent или Workflow напрямую и не требует Mattermost room.
Notification policy создаёт отдельную optional delivery после core result.

## Probes

`/healthz` проверяет process. `/readyz` читает локальный worker/authority
snapshot. Control-plane не вызывается на каждую Kubernetes probe; его outage
делает текущий reconcile retryable `Unavailable`, но не маскируется как local
process failure.

## Инварианты

- один `(scheduleRef, scheduledFor)` материализует один core Run;
- claim связан с workload/method/version/generation/fence/input;
- повтор истёкшего claim повышает generation и заменяет lease/fence;
- disable/archive/cancel target закрывает leases одной owner transaction;
- actor, project и target owner не принимаются из worker payload;
- scheduler не создаёт Pod и не вычисляет Run graph.

При incident проверять safe occurrence ref, due time/timezone, attempt/fence,
lease expiry, Run receipt и stable error code. Не запускать schedule вручную
через SQL и не использовать ручной Control Center launch как скрытый retry.
