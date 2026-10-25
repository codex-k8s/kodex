---
id: OPS-DOC-TERMINAL-GATE-1873
title: Окончательный отказ закрытого Gate после deadline
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-10-25
---

# Закрытый Gate и исторические часы

Issue [#1873](https://github.com/codex-k8s/kodex/issues/1873), эпик #1830.
Позднее решение после штатного timeout возвращало retryable503: deadline reader
находил сохранённые истёкшие часы terminal root, следующий active-owner query
возвращал `pgx.ErrNoRows`, а adapter безусловно преобразовывал это в Unavailable.
До существующего `resolveGate`/AlreadyResolved команда не доходила.

Системный аналог: APPROVED Gate завершённого дочернего этапа может сохранять
старый step deadline, пока coordinator и root ещё работают. Закрытый Gate
не должен инициировать expiry активного root по историческим часам этапа.
Это следует из прежнего owner-контракта: только OPEN Gate допускается к решению.

Путь: HTTP resolution → CP ResolveOwnerGate → owner Execute → project
serialization → fresh actor/organization/project/resource authority → receipt
replay → closed-only Gate recheck → обычный deadline barrier → resolveGate.
Closed-only query блокирует только строки `state <> OPEN`, в exact tenant/ref;
OPEN-строки не получают новой блокировки перед существующим graph closure.
Дополнительная проверка закрытия выполняется перед expiry effect и при
отсутствии active owner. Реальная SQL failure и неизвестное состояние не
превращаются в skip. Миграции, API/Proto, grants и политика timeout не меняются.

| Случай | Авторитетный результат и эффекты |
| --- | --- |
| OPEN, срок ещё не наступил | Прежние lock/OCC/allowed decisions и переход; version mismatch сохраняется. |
| OPEN, срок наступил, root активен | Прежнее атомарное FAILED/RUNTIME_TIMEOUT всего графа, CANCELLED Gate и отзыв leases/invocations; запрошенный effect запрещён. Expiry receipt сохраняет прежний403. |
| Закрытый Gate, root terminal | AlreadyResolved → gRPC FailedPrecondition → HTTP409 STATE_CONFLICT, retryable=false; никаких новых receipt/audit/events/turns/effects. |
| Закрытый child Gate, root ещё активен | Тот же409 до чтения исторических clocks; root/child/revision/grants не меняются. |
| Успешный same-key receipt replay | Прежний receipt после свежей authority, до нового closed check; эффект не повторяется. |
| Новый ключ после закрытия |409; ключ не создаёт право на повтор. |
| Конкурентный claim-expiry и resolution | Каноническая project serialization и recheck; ровно один полный набор terminal events. |
| Foreign/unknown resource | Прежний скрытый NotFound до receipt/clock read; состояние чужого Gate не раскрывается. |
| Storage failure либо неожиданный miss/state | Unavailable, без пропуска barrier, синтетического успеха либо нового эффекта. |

Lease-based inputs в том же deadline dispatcher сначала проходят прежний
exact lease/fence/generation resolver; закрытые leases отклоняются до clocks.
Новый Gate-specific guard к ним не применяется. Integration и Interaction Gate
сохраняют свои owner scopes; их прежний closed-state AlreadyResolved совпадает
с этим guard. Новый background task, retry, event либо grant не вводится.

Адресный unit: `TestRuntimeDeadlineTerminalGate`. Компонентная проверка
использует только новый subtest существующей proper-delegation оснастки:

```sh
GOTOOLCHAIN=go1.26.6 GOMAXPROCS=2 \
  timeout --signal=TERM --kill-after=10s 240s \
  scripts/tests/control-plane-postgres-test.sh \
  '^TestWorkflowLaunchComponent$/^terminal-gate-deadline$'
```

Runner использует фактический `-p 2`; fixture ограничена90секундами. Должны
выполниться один top-level component, один адресный subtest и пять случаев:
active expiry, poll expiry, approved terminal, approved active root и concurrent
expiry. Старые subtests не запускаются. Clocks назначаются только обычным claim;
тест не изменяет время, business state или clocks SQL-записью. Сравниваются
точные aggregate/ref/state/summary tuples всех expiry events и неизменность
graph/turns/leases/invocations/receipts/audit/outbox после отрицательных команд.

Ручная приёмка после отдельной поставки: fresh GET уже CANCELLED Gate, один
APPROVE с актуальным ETag и новым ключом →409/nonretryable, неизменные Gate/run
version/sequence и отсутствие продолжения/внешних действий. Запрос с неизвестным
исходом не повторяется. Rollback — предыдущий CP image; исходный503 вернётся,
но закрытое состояние не должно измениться. Локальные проверки не заменяют live
приёмку; фактические результаты привязываются к exact commit в handoff.
