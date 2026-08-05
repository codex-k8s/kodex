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
| `control.automation-scheduler.readiness` | `CheckReadiness` | Готовность того же защищённого пути |
| `control.schedule.claim-due` | `ClaimDueSchedules` | PostgreSQL clock, cron/interval/timezone, misfire/overlap и immutable occurrence |
| `control.schedule.claim-occurrence` | `ClaimScheduleOccurrence` | Одна owner transaction создаёт root Session/Turn/ProcessRun/RuntimeRevision и scheduler lease |
| `control.schedule.complete-occurrence` | `CompleteScheduleOccurrence` | Сверяет terminal owner graph и применяет retry/backoff/dead-letter |

`ManageSchedule`, `CancelScheduleOccurrence`, `ResolveOwnerGate`, runtime RPC,
Mattermost API и Kubernetes API в operation profile job отсутствуют.

## Сквозная карта

| Этап | Авторитетный путь |
| --- | --- |
| Инициатор | Владелец через `control-api-gateway`; actor/organization/project разрешает `control-plane`, не request payload |
| Расписание | `ManageSchedule` валидирует cron либо interval, IANA timezone и pinned target/prompt/runtime; первый `next_run_at` вычисляет по PostgreSQL time. `RunScheduleNow` создаёт отдельную occurrence и не меняет этот watermark |
| Producer | `automation-scheduler` с mTLS SPIFFE `.../sa/automation-scheduler`, organization-scoped application grant и локальным UDS issuer вызывает только закрытый operation set; проект выбирает owner-side durable round-robin cursor |
| Due | `ClaimDueSchedules` использует PostgreSQL clock, двигает `next_run_at` и сохраняет детерминированный occurrence key `(schedule_id, scheduled_for)` в одной transaction |
| Enqueue | `ClaimScheduleOccurrence` блокирует owner graph и атомарно создаёт `Session -> RuntimeRevision -> Turn -> ProcessRun? -> ScheduledRun`, receipt, audit и lease |
| Исполнение | `agent-runner` claims Turn и принимает только закрытый scheduled outcome `no_action/action_taken/requires_human/failed`; `runtime-controller` владеет RuntimeExecution/Pod path. Scheduler не создаёт Pod и не запускает AI |
| Owner gate | Runtime owner transaction переводит exact RuntimeExecution/Turn/ProcessRun/occurrence/run в `WAITING_OWNER`, создаёт один OwnerGate с exact schedule room; `interaction-gateway` отдельно claims/delivers durable notification |
| Решение | Только exact root owner decision либо expiry создаёт следующий server-owned переход; scheduler продолжает polling, но не интерпретирует transport response как terminal authority |
| Завершение | `CompleteScheduleOccurrence` принимает scheduler lease лишь после terminal Turn/ProcessRun и одной transaction закрывает run либо создаёт свежую retry attempt |

## Полный lifecycle

| Состояние/событие | Владелец и эффект |
| --- | --- |
| create | `ManageSchedule(CREATE)` назначает owner, версию, next run и pinned input |
| manual | `RunScheduleNow` под owner/version/idempotency lock создаёт отдельную немедленную occurrence; плановый watermark не меняется |
| due | Job вызывает `ClaimDueSchedules`; PostgreSQL time определяет eligibility |
| misfire | Control-plane применяет `SKIP`, `RUN_ONCE`, `CATCH_UP` или `WITHIN_GRACE` |
| overlap | `FORBID` сохраняет due watermark, `SKIP` фиксирует terminal audit, `QUEUE` сохраняет FIFO |
| claim/enqueue | Один победитель получает lease; root execution graph фиксируется атомарно |
| watch | Job transiently хранит только полученный lease и повторяет completion с тем же semantic key |
| terminal | Только terminal Turn/ProcessRun позволяет success/failure/cancel disposition |
| retry/backoff | `FAILED/EXPIRED` создаёт fresh attempt/Turn/RuntimeRevision/grant после server-side backoff; root ProcessRun остаётся тем же, ScheduledRun хранит историю attempt |
| dead-letter | Исчерпание attempts/deadline либо некорректная нематериализованная строка получает owner-side `DEAD_LETTER` и audit |
| pause/resume | Только `ManageSchedule(PAUSE/ACTIVATE)`; queued retry сохраняется, новый claim ждёт ACTIVE |
| requires_human | Exact graph остаётся `WAITING_OWNER`; без решения владельца completion не проходит |
| owner decision/expiry | Отдельные owner paths имеют one-winner locks и закрывают gate/runtime/scheduler authority вместе |
| lease expiry | Следующий claim запускает PostgreSQL watchdog; restart job не нужен для сохранности состояния |
| cancel/delete | Только специализированные owner commands после полного graph lock; scheduler их не вызывает |

## Transaction, idempotency, fence и one-winner

| Операция | Transaction/lock | Idempotency | Fence/lease | One-winner |
| --- | --- | --- | --- | --- |
| due materialization | После SQL eligibility Schedule rows `FOR UPDATE SKIP LOCKED` + occurrence unique key | process key сохраняется при unknown outcome; ротация short-lived bearer не меняет semantic intent | Schedule version и PostgreSQL clock | unique occurrence + row lock |
| occurrence claim | Organization grant → server-owned project partition → canonical occurrence → Schedule → graph locks | один key на попытку RPC; exact replay возвращает тот же lease только пока binding live | exact project+occurrence+attempt, authority generation, claim-key hash, token hash, lease deadline | eligibility применяется до `LIMIT`; `SKIP LOCKED`, blocker query и OCC update |
| completion | полный текущий execution graph до receipt | UUIDv5 от occurrence+attempt; одинаков после restart | exact attempt/token/current tuple | terminal owner graph либо watchdog, не HTTP response |
| invalid row isolation | отдельная owner transaction после rollback; повреждённая expired binding восстанавливается из immutable ScheduledRun, а доказанно отсутствующий graph получает dead-letter | проверяет отсутствие receipt текущего claim key либо maintenance receipt occurrence+attempt | `QUEUED` без claim/execution получает quarantine; потенциально живой graph не терминализируется частично | repair/dead-letter имеют cardinality-one audit; остальные строки того же schedule продолжаются |
| owner gate | RuntimeExecution -> occurrence -> Schedule -> run -> Session -> Turn -> ProcessRun -> Gate | server-owned decision/delivery receipts | current tuple, delivery fence и deadline | решение/expiry имеет одного победителя |

Локальные часы используются только для удаления просроченного transient claim
из памяти после server-issued deadline. Они не разрешают due, terminal, retry
или lease transition. После restart durable PostgreSQL state и watchdog
восстанавливают путь без local checkpoint.

## Producer, consumer, readiness и deploy

| Звено | Материализация |
| --- | --- |
| Producer/client | Этот Go module, generated `controlplaneapi`, `controlplaneclient.AutomationSchedulerOperations`, mTLS + organization-only application grant без project scope + UDS issuer; shared authority binary содержит закрытый `automation-scheduler` workload profile |
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
ScheduledRun либо dead-letter после доказанного отсутствия execution graph.
Потенциально исполняемые Session/Turn/ProcessRun не закрываются частично.

Ошибка общей зависимости возвращается на job boundary одним error log и не
создаёт busy loop: следующий запуск происходит только по ticker. Повторы
`Unavailable`/`DeadlineExceeded` ограничены двумя попытками с тем же
семантическим idempotency key. Если transport outcome остался неизвестным,
следующий цикл сохраняет тот же due/claim key до authoritative ответа; после
restart duplicate блокируют owner receipt и occurrence unique key.

## Примеры и эксплуатация

Проверяемые, но не предназначенные для `kubectl apply` примеры находятся в
[`examples/periodic-schedules.yaml`](examples/periodic-schedules.yaml):
почасовой mailbox manager использует interval, а daily improver — cron и
явный recursion guard в pinned playbook. Placeholder UUID перед созданием
заменяет owner UI/Git configuration path. Указанный в fixture `next_run_at`
не является authority: control-plane вычисляет его заново из
cron/interval/timezone.

Runbook: [`docs/runbooks/automation-scheduler.md`](../../../docs/runbooks/automation-scheduler.md).
Канонический render выполняет `tools/render-automation-scheduler.sh`; он не
применяет manifest в кластер.
