---
id: JOB-MC-001
title: Automation scheduler
type: service
status: approved
owner: backend
version: 1.0.0
updated: 2026-08-23
---

# Automation scheduler

`automation-scheduler` — stateless job, которая будит авторитетный контур
расписаний `control-plane`. Она не хранит Schedule, не вычисляет cron, не
создаёт Run локально и не запускает agent Pod.

## Рабочий путь

1. `ClaimDueSchedules` выбирает ограниченный набор наступивших occurrences по
   PostgreSQL clock, фиксирует immutable snapshot и выдаёт lease с fence и
   монотонным generation.
2. `MaterializeScheduleOccurrence` сверяет exact occurrence/lease/fence,
   semantic idempotency key и актуальность target, после чего одной
   owner-транзакцией создаёт Run с source `SCHEDULE`.
3. Исполнение, Human Gate, artifacts, cancel и terminal lifecycle дальше
   принадлежат `control-plane` и обычному runtime-контуру. Scheduler их не
   интерпретирует и не завершает самостоятельно.

Повтор materialization с тем же occurrence использует стабильный semantic key
и не создаёт второй Run. Истёкший claim переиздаётся с новым generation;
прежний lease закрыто отклоняется. Отключённый Schedule либо недоступный target
не материализуются.

## Граница полномочий

Профиль job содержит только операции:

- `platform.runtime.schedules.claim`;
- `platform.runtime.schedules.materialize`.

Каждый RPC проходит mTLS, application grant, локальный UDS issuer, authority
proof и exact operation check. Organization, Project, actor, target version и
lineage не принимаются от job как authority: их разрешает `control-plane` из
сохранённого Schedule и occurrence snapshot.

## Готовность и отказы

`/healthz` подтверждает жизнь процесса. `/readyz` читает локальный снимок,
который фоновый monitor строит только по состоянию собственного issuer sidecar.
Недоступность `control-plane` не делает Pod неготовым: рабочий цикл получает
typed `Unavailable`, один раз пишет переход в degraded и продолжает bounded
polling; восстановление также логируется один раз.

Deployment находится в `deploy/k8s/base/automation-scheduler`. Job не требует
Mattermost, GitHub, Kubernetes API либо внешних credentials. Runbook:
[`docs/runbooks/automation-scheduler.md`](../../../docs/runbooks/automation-scheduler.md).
