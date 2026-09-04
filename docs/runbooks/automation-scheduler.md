---
id: RUN-MC-016
title: Диагностика automation-scheduler
type: runbook
status: approved
owner: sre
version: 3.0.0
updated: 2026-09-04
---

# Диагностика automation-scheduler

Scheduler является worker, а не владельцем Schedule. Control-plane хранит
target, timezone, preset/cron, input, session/notification policies,
occurrences, attempts и lifecycle.

## Цикл

1. Claim exact due occurrence по generated RPC.
2. Получить occurrence/version/generation/fence/immutable input digest.
3. Продлить lease, затем попросить control-plane создать Run source `SCHEDULE`.
4. При ошибке закрыть attempt через `FailScheduleOccurrence`; после expiry
   следующий worker создаёт новую attempt/generation, не переиспользует старую.
5. Завершить цикл: terminal RuntimeWork transaction сама обновит occurrence и
   Schedule вместе с авторитетным Run graph.

Schedule запускает Agent или Workflow напрямую и не требует Mattermost room.
Notification policy сохраняется в revision; optional delivery не является
условием создания core result и не выполняется scheduler.

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

## Восстановление

`last_outcome` в защищённом Get/List Schedule показывает состояние последней
occurrence и безопасный код ошибки. `SKIPPED` не создаёт Run. `RETRY_WAIT`
автоматически выдаётся другой реплике. После трёх попыток либо постоянной ошибки
`DEAD_LETTER` блокирует новые occurrences независимо от overlap policy.
Оператор устраняет причину и явно выключает/включает Schedule с актуальными
version и idempotency key. Выключение закрывает незавершённые scheduler attempts;
включение пересчитывает следующий due. При изменении target сначала сохранить
обновлённый Schedule, чтобы создать новую pinned revision. Архивирование
не останавливает уже материализованный Run: его отменяют отдельной командой Run.

`AutomationSchedulerCycleFailures` означает ошибки RPC/authority рабочего цикла.
`AutomationSchedulerOccurrenceFailures` отделяет invalid snapshot, renew и
materialize failures. `AutomationSchedulerProtectedPathUnavailable` проверяет
локальный issuer, а не доступность бизнес-сервиса. `tracked_claims` имеет
значение 0 или 1 на реплику; это не размер очереди в PostgreSQL.

Миграция сохраняет прежние revisions, добавляет новую с актуальным target для
существующих Schedule и закрывает leases старого протокола. Неразрешимый target
выключается. Историческим Run не приписывается неизвестная прежняя target revision.

До rollout необходимы migration `20260904000400`, policy revision 44 и образы
с одной версией Proto. Render заменяет один scheduler image и три authority
images (issuer, grant-agent, socket-init). Реестр, CA, signer, workload TLS,
telemetry и network destinations принадлежат существующему platform profile;
новые права и доступ к PostgreSQL scheduler не получает.

При регрессии сначала выключить расписания через защищённый API. Миграция
forward-only: down и ручное редактирование immutable history запрещены.
Возврат старого worker допустим только после проверки совместимости его RPC
с текущим control-plane; иначе оставить worker остановленным до исправления.
Локальная проверка: `make test-automation-scheduler`, без apply/deploy.
