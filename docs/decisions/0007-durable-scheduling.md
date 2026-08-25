---
id: ADR-MC-007
title: Долговечное планирование в PostgreSQL
type: decision
status: approved
owner: architect
version: 1.0.0
updated: 2026-08-23
---

# ADR-MC-007. Долговечное планирование в PostgreSQL

## Решение

`Schedule`, `ScheduleOccurrence` и состояние следующего запуска принадлежат
control-plane и хранятся в PostgreSQL. `automation-scheduler` только получает
fenced claim наступившего occurrence и просит владельца materialize-ить один
`Run`. Kubernetes CronJob и локальный таймер worker-а не являются источником
бизнес-состояния.

Долговечные определения, вычисленный due time, timezone, политики пропущенных
запусков и параллельности, attempts и история occurrences принадлежат
Kodex. Реализация внутренней очереди не изменяет этот контракт.

При claim control-plane одной транзакцией фиксирует immutable input/target
snapshot и digest occurrence, выдаёт fenced lease и вычисляет следующий due
time. Worker не интерпретирует cron и не пересчитывает timezone. Изменение
Schedule после claim влияет только на будущие occurrences.
Истечение claim не отменяет уже наступивший occurrence: owner повторно выдаёт
его с новым lease/fence и монотонным поколением, а старое поколение теряет
полномочия.

## Последствия

- Несколько реплик работают без повторных экземпляров расписания.
- Сессия по расписанию без чата является полноценным источником запуска.
- На первом этапе не появляется отдельная зависимость от Redis или Temporal.
- Temporal может быть рассмотрен позже для сложных детерминированных процессов.
