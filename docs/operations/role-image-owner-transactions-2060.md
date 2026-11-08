---
id: OPS-DOC-2060
title: Повтор owner-команд рецепта и записи допуска образа
type: runbook
status: approved
owner: sre
version: 1.0.0
updated: 2026-11-08
---

# Owner-транзакции RoleImage

Источник: [#2060](https://github.com/codex-k8s/kodex/issues/2060), продолжение
[#1977](https://github.com/codex-k8s/kodex/issues/1977). Один deployable unit:
`control-plane`. Worker build lifecycle уже покрыт
[#2050](role-image-worker-transactions-2050.md); диагностика admission Job относится
к отдельной #2042. Изменение не требует миграций, новых deployment flags,
пересборки внешнего scanner либо изменения policy.

## Граница и жизненный цикл

| Сценарий | Инициатор и вход | Owner-проверки каждой попытки | Эффект и потребитель |
| --- | --- | --- | --- |
| Команда recipe PROJECT | Проверенный gateway principal, специализированный manage RPC, server-selected scope | Actor/tenant, project/role access, managed lineage, exact mutation intent/key, recipe OCC | Атомарные recipe/build, audit, receipt и прежнее `ROLE_IMAGE_RECIPE_CHANGED`; read projection и существующий builder |
| Команда recipe ORGANIZATION | Тот же RPC через organization endpoint, без project locators | Дополнительно прежний organization access; scope не берётся из prompt | Та же owner-транзакция и прежние consumers |
| REQUEST_BUILD | Явная команда с exact recipe version | Те же owner-проверки; eligibility/lineage не кэшируются | Одна новая owner build; BuildKit/push исполняются позже worker, вне retry callback |
| Запись ACCEPTED/REJECTED | Проверенный admission workload, exact artifact/attempt/fence/lease/policy и immutable evidence pins | Scoped receipt, artifact OCC, текущая live attempt, authority revision, token, current policy, inventory и report | Атомарные artifact/attempt closure, vulnerability report/history, receipt/audit и прежнее событие; promotion остаётся отдельным закрытым сценарием |
| Exact replay после потери ответа | Прежние actor/tenant/key/intent | Прежний receipt path; для manage авторизация предшествует replay | Сохранённый ответ, без нового build/report/event |
| Stale/cancel/revoke/expiry/foreign scope | Прежние specialized commands и current owner state | Существующие проверки выполняются заново; повтор не возрождает lease/attempt | Отказ без новых effects; terminal/expiry readers и controller recovery не меняются |
| Неизвестный COMMIT | Потерянный transport acknowledgement | Автоматический повтор запрещён | `Unavailable`; caller сверяет exact receipt/read path, не повторяет внешний effect вслепую |

Повтор охватывает всю SERIALIZABLE-транзакцию со свежим snapshot, не отдельный
SQL UPDATE. Используется существующий loop максимум на три попытки с ожиданиями
5/10ms и исходным caller context. Только типизированные PostgreSQL40001/40P01
и известный server abort разрешают повтор. Rollback завершается до следующей
попытки с отдельным ограниченным контекстом1s; его отказ снимает retry marker.
Обычный Unavailable, неизвестный COMMIT, OCC, permission/policy/token rejection
и постоянная SQL-ошибка не повторяются. Partial result не выдаётся.

Существующий формат idempotency не меняется. Для `RecordAdmission` exact replay
сохраняет весь прежний envelope, включая correlation ID; произвольный новый
correlation ID не считается тем же intent. Нельзя заявлять такой replay
проверенным или менять старые receipts ради него. `Manage` использует прежний
server-materialized mutation intent и допускает новый transport correlation.

Обновляется только control-plane. Остальные component pins, CA/mTLS/JWS,
registry, image/file admission и storage сохраняются. Rollback к старому image
не требует DDL rollback: схемы и payloads прежние, но известный дефект возвратится.
Actual deployment требует собственных immutable image pins и проверки совместимости.

## Проверки

`TestRoleImageOwnerTransactionRetryComponent` использует отдельную случайную БД,
скопированную из пустого migrated template: настоящие CREATE/REQUEST_BUILD и
claim/record с synthetic inventory/report. Нативный SQL-trigger выдаёт40001
или40P01 на installation sequence append после предыдущих owner writes.
Проверяются PROJECT/ORGANIZATION, ACCEPTED/REJECTED, две фактические SQL-попытки,
один committed build/report/receipt/event и неизменность state при exact replay.
Nontransactional sequence доказывает число попыток, а не число вызовов fake.

`TestRoleImageOwnerTransactionRetryBoundsComponent` проверяет исчерпание трёх
попыток, постоянный SQLSTATE23505, stale version, foreign tenant, cancellation
и неверные admission token/fence/attempt/policy. Snapshot owner state остаётся
прежним. Unit `TestRoleImageOwnerCommitRetryBoundary` проверяет actual commit
classifier вместе с loop/cleanup: unknown acknowledgement и cleanup failure
не повторяются, private driver text не выдаётся.

```sh
scripts/tests/control-plane-postgres-test.sh '^TestRoleImageOwner(TransactionRetry|CommitRetry)'
```

Оснастка запускает только disposable PostgreSQL с synthetic configuration,
не scanner и не внешние APIs. Проверены документы pgx v5.10.0 через Context7:
BeginTx/Commit/Rollback и безопасный Rollback уже закрытой transaction. Решение
о допустимости повтора остаётся у owner, библиотека его автоматически не принимает.

Ручная installed-проверка: закрепить новый control-plane digest в разрешённой
одноразовой установке, выполнить одну synthetic build → scan → record, прочитать
owner verdict/report и убедиться в отсутствии дублированных effects. Успешный
merge, unit/native tests или Ready Pod не доказывают installed qualification.
