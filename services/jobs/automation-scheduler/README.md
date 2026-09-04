---
id: JOB-MC-001
title: Automation scheduler
type: service
status: approved
owner: backend
version: 2.0.0
updated: 2026-09-04
---

# Automation scheduler

`automation-scheduler` — stateless job, которая будит авторитетный контур
расписаний `control-plane`. Она не хранит Schedule, не вычисляет cron, не
создаёт Run локально и не запускает agent Pod.

## Рабочий путь

1. `ClaimDueSchedules` выбирает ограниченный набор наступивших occurrences по
   PostgreSQL clock, фиксирует immutable snapshot и выдаёт lease с fence и
   монотонным generation.
2. `RenewScheduleOccurrence` продлевает только действующую попытку с теми же
   workload credential generation, occurrence, lease, fence и generation.
3. `MaterializeScheduleOccurrence` сверяет exact occurrence/lease/fence,
   semantic idempotency key и актуальность target, после чего одной
   owner-транзакцией создаёт Run с source `SCHEDULE`.
4. `FailScheduleOccurrence` закрывает попытку; retryable ошибка создаёт
   `RETRY_WAIT`, исчерпание трёх попыток либо постоянная ошибка - `DEAD_LETTER`.
5. Исполнение, Human Gate, artifacts, cancel и terminal lifecycle дальше
   принадлежат `control-plane` и обычному runtime-контуру. Scheduler их не
   интерпретирует и не завершает самостоятельно.

Повтор materialization с теми же occurrence и generation использует стабильный semantic key
и не создаёт второй Run. Истёкший claim переиздаётся с новым generation;
прежний lease закрыто отклоняется. Отключённый Schedule либо недоступный target
не материализуются. При архивации `control-plane` атомарно отменяет будущие
occurrences в `DUE|CLAIMED|RETRY_WAIT|DEAD_LETTER`, отзывает их leases и очищает `next_run_at`.
Архивированный Schedule остаётся доступен только для чтения истории и больше
не попадает в claim или materialization path; уже созданный Run продолжает
собственный lifecycle.

## Граница полномочий

Профиль job содержит только операции:

- `platform.runtime.schedules.claim`;
- `platform.runtime.schedules.renew`;
- `platform.runtime.schedules.materialize`;
- `platform.runtime.schedules.fail`.

Каждый RPC проходит mTLS, application grant, локальный UDS issuer, authority
proof и exact operation check. Organization, Project, actor, target version и
lineage не принимаются от job как authority: их разрешает `control-plane` из
сохранённого Schedule и occurrence snapshot.

## Расписание и snapshot

`HOURLY|DAILY|WEEKLY|CUSTOM` используют один пятикомпонентный cron parser и
IANA timezone. DST gap переносится на первую существующую минуту после разрыва;
DST fold исполняется только в первое вхождение. Preview, create и due claim
используют одну реализацию `domain/service/schedule`.

`COALESCE` материализует одну сохранённую due occurrence и переносит next после
текущего времени. `CATCH_UP_ONE` последовательно догоняет пропуски.
`SKIP` сохраняет terminal receipt без Run и пропускает опоздание от минуты;
обычная задержка polling внутри минуты не теряет запуск. `FORBID` исключает
параллельные незакрытые occurrences, `ALLOW` разрешает их. `DEAD_LETTER`
останавливает новые claims этого Schedule до явного выключения/включения.

Revision хранит cron/policies, точные target version/digest, Automation text,
input и prompt inputs. Occurrence и append-only attempts сохраняют это
происхождение; materialization фиксирует один Run/Session/Turn. Общий renderer
получает Automation из owner-state, не из worker payload. Workflow получает
задачу через coordinator, без добавления полей в закрытую входную схему.
`CONTINUE_ONE` использует сохранённую совместимую сессию. Обычный `RetryRun`
наследует provenance по серверному `retry_of` и получает свежий RuntimeRevision.

## Воспроизводимая проверка

### Матрица жизненного цикла

Источник: Issue #1027/#1018, GUIDE-DOC-006. Для браузера actor и tenant приходят
из OIDC session gateway; для worker - из mTLS, application grant и проверенного
exact-method proof. Владелец всех перечисленных транзакций - control-plane.

| Инициатор и внешний путь | RPC / переход | Версия, receipt и эффект | Событие и потребитель |
| --- | --- | --- | --- |
| Браузер, POST `/api/v1/schedules/preview` | PreviewSchedule | Без записи; тот же parser/policies; invalid argument для неверной спецификации | Нет; ответ браузеру |
| Браузер, POST project schedules | CreateSchedule | Permission проекта, server owner/target, immutable revision, idempotency/audit | Один SCHEDULE_CHANGED в outbox; PWA перечитывает Schedule |
| Браузер, PUT schedule | UpdateSchedule | Tenant/owner, If-Match, новый snapshot и receipt; прежний claim сохраняет свою revision | Один SCHEDULE_CHANGED; PWA |
| Scheduler, без публичного endpoint | ClaimDueSchedules: due -> CLAIMED | DB clock, row lock, один due key, attempt/lease/fence/digests/credential generation | Один SCHEDULE_CHANGED на новую attempt; protected Get/List Schedule |
| Scheduler | RenewScheduleOccurrence | Только live exact lease; та же attempt/generation, новый expiry | Нет; авторитетный RPC возвращает expiry |
| Scheduler | MaterializeScheduleOccurrence: CLAIMED -> MATERIALIZED | Один Run/Session/Turn, stable key на occurrence+generation, audit; late fence отклоняется | Атомарный Run graph/outbox; runtime и PWA |
| Scheduler | FailScheduleOccurrence: CLAIMED -> RETRY_WAIT/DEAD_LETTER | Закрытая attempt/lease; повтор command возвращает receipt | Один SCHEDULE_CHANGED; protected Get/List Schedule |
| Другая реплика | RETRY_WAIT/expired -> CLAIMED | Новая attempt, generation и lease; максимум три | Один SCHEDULE_CHANGED на новую attempt; protected Get/List Schedule |
| DB clock через scheduler | Последний expiry -> DEAD_LETTER | Закрывает attempt/lease, ставит terminal time; новые due заблокированы | Один SCHEDULE_CHANGED; protected Get/List Schedule |
| DB clock через scheduler | SKIP -> SKIPPED | Terminal receipt без Run/lease | Один SCHEDULE_CHANGED; last_outcome |
| Браузер, schedule commands/delete; target lifecycle | Pause/archive/delete | Owner/OCC/receipt; закрывает scheduler attempts и leases; уже созданный Run независим | SCHEDULE_CHANGED; PWA; soft delete сохраняет историю |
| Runtime callback / браузер Run cancel | MATERIALIZED -> COMPLETED/FAILED/CANCELLED | Run graph и occurrence закрываются одной owner-транзакцией | Run events; runtime/PWA; Schedule читается через защищённый API |
| Браузер, Run retry | Новый Run attempt | Серверное retry_of; новый turn, grant и RuntimeRevision | Run events; runtime/PWA |
| Runtime / owner gate | WAITING_OWNER / CHANGES_REQUESTED | Scheduler не выдаёт второй Run; runtime/owner владеет продолжением и grants | Run/gate events; runtime/PWA |

Platform events являются invalidation-сигналами с durable sequence, а не
исполняемым snapshot. Worker не исполняет их payload: каждый запуск получает
точный snapshot из защищённого claim и RuntimeRevision. Renew не создаёт
новое событие или attempt. Fences и тексты Automation не попадают в метрики.

`make test-automation-scheduler` запускает targeted Go/race, disposable PostgreSQL
и итоговый render staging/production без доступа к кластеру. Требуются Go из
Makefile, Docker, kubectl, yq v4 и jq. Бюджет каждой группы ограничен скриптом.
Контракты отдельно: `make gen-proto check-proto-codegen lint-proto build-proto
test-authority-policy-codegen`. Версии генераторов закреплены в Makefile;
generated файлы вручную не изменяются. Cron API сверены с Context7 `/robfig/cron`.

## Готовность и отказы

`/healthz` подтверждает жизнь процесса. `/readyz` читает локальный снимок,
который фоновый monitor строит только по состоянию собственного issuer sidecar.
Недоступность `control-plane` не делает Pod неготовым: рабочий цикл получает
typed `Unavailable`, один раз пишет переход в degraded и продолжает bounded
polling; восстановление также логируется один раз.

Deployment находится в `deploy/k8s/base/automation-scheduler`. Job не требует
Mattermost, GitHub, Kubernetes API либо внешних credentials. Runbook:
[`docs/runbooks/automation-scheduler.md`](../../../docs/runbooks/automation-scheduler.md).
