# Automation scheduler

`automation-scheduler` — непрерывная bounded job Issue
[#193](https://github.com/codex-k8s/matter-codex/issues/193). Она будит
авторитетный scheduler path `control-plane`, но не хранит расписания, не
разбирает cron, не вычисляет backoff и не исполняет agent process.

## Защищённые виды и команды

Job работает только с server-owned `Schedule`, `ScheduleOccurrence` и
`ScheduledRun`. Материализованные `Session`, `RuntimeRevision`, `Turn`,
`ProcessRun` и `OwnerGate` доступны ей только как результат owner transaction;
изменять их универсальными командами job не может.

| Operation ID | Generated RPC | Назначение |
| --- | --- | --- |
| `control.automation-scheduler.readiness` | `CheckReadiness` | Готовность отдельного минимально привилегированного readiness path |
| `control.schedule.claim-due` | `ClaimDueSchedules` | PostgreSQL clock, cron/interval/timezone, misfire/overlap и immutable occurrence |
| `control.schedule.claim-occurrence` | `ClaimScheduleOccurrence` | Organization credential только резервирует одну server-selected occurrence и получает exact materialization capability |
| `control.schedule.materialize-occurrence` | `MaterializeScheduleOccurrence` | Одноразовая capability создаёт root Session/Turn/ProcessRun/RuntimeRevision и выдаёт отдельную completion capability |
| `control.schedule.complete-occurrence` | `CompleteScheduleOccurrence` | Exact completion capability сверяет terminal owner graph и применяет retry/backoff/dead-letter |

`ManageSchedule`, `CancelScheduleOccurrence`, `ResolveOwnerGate`, runtime RPC,
Mattermost API и Kubernetes API в operation profile job отсутствуют.

## Сквозная карта

| Этап | Авторитетный путь |
| --- | --- |
| Инициатор | Владелец через `control-api-gateway`; actor/organization/project разрешает `control-plane`, не request payload |
| Расписание | `ManageSchedule` валидирует cron либо interval, IANA timezone и pinned target/prompt/runtime; первый `next_run_at` вычисляет по PostgreSQL time. `RunScheduleNow` создаёт отдельную occurrence и не меняет этот watermark |
| Producer | `automation-scheduler` с mTLS SPIFFE `.../sa/automation-scheduler`, отдельными readiness и organization-scoped operational application grants и локальным UDS issuer вызывает только закрытый operation set; проект выбирает owner-side durable round-robin cursor |
| Due | `ClaimDueSchedules` использует PostgreSQL clock, двигает `next_run_at` и сохраняет детерминированный occurrence key `(schedule_id, scheduled_for)` в одной transaction |
| Reservation | `ClaimScheduleOccurrence` не создаёт execution graph: owner выбирает project/occurrence, фиксирует `RESERVED` и одноразовую capability exact server JTI/digest/project/occurrence/attempt/input/generation/full method/workload/SPIFFE |
| Enqueue | `MaterializeScheduleOccurrence` потребляет capability и атомарно создаёт `Session -> RuntimeRevision -> Turn -> ProcessRun -> ScheduledRun`; отдельная completion capability хранится только как digest и lifecycle state |
| Исполнение | `RuntimeRevision` и `Turn` закрепляют один `scheduled-result.v1`; `runtime-controller` передаёт его из generated read path в runner input, а `agent-runner` принимает только закрытый outcome `no_action/action_taken/requires_human/failed`. Scheduler не создаёт Pod и не запускает AI |
| Owner gate | Runtime owner transaction переводит exact RuntimeExecution/Turn/ProcessRun/occurrence/run в `WAITING_OWNER`, создаёт один OwnerGate с exact schedule room; `interaction-gateway` отдельно claims/delivers durable notification |
| Решение | Только exact root owner decision либо expiry создаёт следующий server-owned переход; scheduler продолжает polling, но не интерпретирует transport response как terminal authority |
| Завершение | `CompleteScheduleOccurrence` потребляет exact capability лишь после terminal Turn/ProcessRun и одной transaction закрывает run либо создаёт свежую retry attempt |

## Полный lifecycle

| Состояние/событие | Владелец и эффект |
| --- | --- |
| create | `ManageSchedule(CREATE)` назначает owner, версию, next run и pinned input |
| manual | `RunScheduleNow` под owner/version/idempotency lock создаёт отдельную немедленную occurrence; плановый watermark не меняется |
| due | Job вызывает `ClaimDueSchedules`; PostgreSQL time определяет eligibility |
| misfire | Control-plane применяет `SKIP`, `RUN_ONCE`, `CATCH_UP` или `WITHIN_GRACE` |
| overlap | `FORBID` сохраняет due watermark, `SKIP` фиксирует terminal audit, `QUEUE` сохраняет FIFO |
| reserve/materialize | Один победитель резервирует occurrence; только server-issued одноразовая capability создаёт root execution graph. Unbound grant не имеет execution authority |
| watch | Job transiently хранит только полученный lease и повторяет completion с тем же semantic key |
| terminal | Только terminal Turn/ProcessRun позволяет success/failure/cancel disposition |
| retry/backoff | `FAILED/EXPIRED` создаёт fresh attempt/Turn/RuntimeRevision/grant после server-side backoff; root ProcessRun остаётся тем же, ScheduledRun хранит историю attempt |
| dead-letter | Исчерпание attempts/deadline либо некорректная нематериализованная строка получает owner-side `DEAD_LETTER` и audit |
| pause/resume | Только `ManageSchedule(PAUSE/ACTIVATE)`; queued retry сохраняется, новый claim ждёт ACTIVE |
| requires_human | Exact graph остаётся `WAITING_OWNER`; без решения владельца completion не проходит |
| owner decision/expiry | Отдельные owner paths имеют one-winner locks и закрывают gate/runtime/scheduler authority вместе |
| lease expiry | Следующий claim запускает PostgreSQL watchdog; restart job не нужен для сохранности состояния |
| recovery blocked | Повреждённый потенциально живой graph переходит в `RECOVERY_BLOCKED`, исключается из bounded watchdog selector и доступен owner через list/readback и `ResolveScheduleRecovery(REPAIR|CANCEL|SKIP)` с exact version/attempt/evidence |
| cancel/delete | Только специализированные owner commands после полного graph lock; scheduler их не вызывает |

## Transaction, idempotency, fence и one-winner

| Операция | Transaction/lock | Idempotency | Fence/lease | One-winner |
| --- | --- | --- | --- | --- |
| due materialization | После SQL eligibility Schedule rows `FOR UPDATE SKIP LOCKED` + occurrence unique key | process key сохраняется при unknown outcome; ротация short-lived bearer не меняет semantic intent | Schedule version и PostgreSQL clock | unique occurrence + row lock |
| occurrence reservation | Organization grant → server-owned project partition → canonical occurrence → Schedule | один stable semantic key; bearer JTI/revision/digest остаются transport replay metadata; owner возвращает закрытый stage `RESERVED/MATERIALIZED/RETIRED` и server-derived materialization key | unbound credential не содержит project/occurrence execution scope; повтор после expiry сохраняет монотонное generation | eligibility применяется до `LIMIT`; `SKIP LOCKED`, blocker query и OCC update |
| materialization/completion | capability row → exact occurrence/current graph до receipt | exact lost-response replay возвращает сохранённый result | server JTI+project+occurrence+attempt+input+generation+full method+workload+SPIFFE+token digest, `ISSUED→CONSUMED/REVOKED` | terminal owner graph либо watchdog, не HTTP response |
| invalid row isolation | отдельная owner transaction после rollback; повреждённая expired binding восстанавливается из immutable ScheduledRun, а доказанно отсутствующий graph получает dead-letter | проверяет отсутствие receipt текущего claim key либо maintenance receipt occurrence+attempt | `QUEUED` без claim/execution получает quarantine; потенциально живой graph не терминализируется частично | repair/dead-letter имеют cardinality-one audit; остальные строки того же schedule продолжаются |
| owner gate | RuntimeExecution -> occurrence -> Schedule -> run -> Session -> Turn -> ProcessRun -> Gate | server-owned decision/delivery receipts | current tuple, delivery fence и deadline | решение/expiry имеет одного победителя |

Локальные часы используются только для удаления просроченного transient claim
из памяти после server-issued deadline. Они не разрешают due, terminal, retry
или lease transition. После restart durable PostgreSQL state и watchdog
восстанавливают путь без local checkpoint.

## Producer, consumer, readiness и deploy

| Звено | Материализация |
| --- | --- |
| Producer/client | Этот Go module, generated `controlplaneapi`, раздельные `controlplaneclient.AutomationSchedulerReadinessOperations` и `controlplaneclient.AutomationSchedulerOperations`, mTLS + readiness grant без бизнесовых прав + organization-only operational grant без project scope + UDS issuer; shared authority binary содержит закрытый `automation-scheduler` workload profile |
| Owner consumer | Существующие caster/domain/repository paths `control-plane`; organization-scoped project cursor, schedule/occurrence/run и runtime result сохраняются только там; прямого PostgreSQL client у job нет |
| Runtime consumer | `agent-runner` и `runtime-controller` забирают созданный Turn/RuntimeExecution своими grants |
| Notification consumer | `interaction-gateway` owner-gate delivery; прямой Mattermost path для scheduler неприменим |
| Async event consumer | Неприменим: job использует только bounded authoritative polling; subscription, inbox и cursor для job не объявляются |
| Readiness | Startup barrier и периодический `Client.Check`: resolver, local issuer и protected `CheckReadiness` |
| Deploy | `deploy/k8s/base/automation-scheduler`, два environment overlay, два replicas, PDB, Vault CSI, exact NetworkPolicy, ServiceMonitor, alerts и dashboard |

## Цикл и отказоустойчивость

Один цикл ограниченно материализует due schedules, независимо согласует все
transient leases и claims следующий пакет. Ошибка completion одной строки
учитывается отдельно и не останавливает остальные claims. Некорректная queued
occurrence откатывает незавершённую materialization transaction, после чего
owner-side quarantine повторно проверяет receipt, blocker и отсутствие
execution binding, фиксирует `DEAD_LETTER` и продолжает selection.
Повреждённая expired binding получает идемпотентный owner-side repair из
ScheduledRun либо `RECOVERY_BLOCKED` с exact owner readback/repair; доказанно
отсутствующий execution graph можно целиком cancel/skip.
Потенциально исполняемые Session/Turn/ProcessRun не закрываются частично.

Ошибка общей зависимости возвращается на job boundary одним error log и не
создаёт busy loop: следующий запуск происходит только по ticker. Повторы
`Unavailable`/`DeadlineExceeded` ограничены двумя попытками с тем же
семантическим idempotency key. Если transport outcome остался неизвестным,
следующий цикл сохраняет тот же due/claim key до authoritative ответа.
`MATERIALIZED` повторно присоединяет этот key к exact сохранённому graph и
completion capability без второго эффекта. Только owner-side `RETIRED` после
expiry либо закрытого stage позволяет job забыть старый claim key; следующий
bounded poll новым key запускает watchdog release/recovery. Permission,
idempotency и tuple mismatch не преобразуются в `RETIRED` или пустой ответ.
После restart duplicate блокируют owner receipt и occurrence unique key.

## Примеры и эксплуатация

Проверяемые, но не предназначенные для `kubectl apply` примеры находятся в
[`examples/periodic-schedules.yaml`](examples/periodic-schedules.yaml):
почасовой mailbox manager использует interval, а daily improver — cron и
явный recursion guard в pinned playbook. Placeholder UUID перед созданием
заменяет owner UI/Git configuration path. `next_run_at` fixture не передаёт:
control-plane вычисляет первый и последующие моменты из cron/interval/timezone
по PostgreSQL time. Оба примера обязаны записать
`mattercodex.scheduled-result.v1`; пустой mailbox/improver возвращает
`no_action`, поэтому `ON_ACTION_OR_FAILURE` не создаёт delivery.

Runbook: [`docs/runbooks/automation-scheduler.md`](../../../docs/runbooks/automation-scheduler.md).
Канонический render выполняет `tools/render-automation-scheduler.sh`; он не
применяет manifest в кластер.
