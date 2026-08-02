---
id: GO-LIB-DOC-009
title: Устойчивый PostgreSQL inbox для Go consumers
type: library
status: approved
owner: architect
version: 1.0.0
updated: 2026-08-02
---

# `postgresinbox`

Пакет реализует общий provider-neutral durable inbox для at-least-once
consumer. Он принимает уже проверенный `eventing.Envelope`, сохраняет
неизменяемое доказательство получения, сериализует обработку одного ключа
порядка и фиксирует локальный PostgreSQL effect, inbox evidence и cursor одной
транзакцией.

Пакет не импортирует broker SDK и не подтверждает сообщения. Broker adapter
владеет фактическими `ACK`, `NACK`, `Term` и redelivery, а получает от пакета
закрытое решение только после устойчивого PostgreSQL commit либо закрытый
отказ, при котором `ACK` запрещён.

## Назначение и границы

Пакет предоставляет:

- durable dedup по точной consumer identity и `eventId`;
- неизменяемый SHA-256 digest полного канонического envelope;
- общий canonical ordering key и непрерывный `eventSequence`;
- состояния receive, claim, effect, complete, retry, dead letter, repair и
  cleanup;
- lease, generation и монотонный fence с единственным победителем;
- ограниченный attempt/repair budget и PostgreSQL-time backoff;
- readiness точного рабочего PostgreSQL path и schema contract;
- закрытые observer/tracer hooks без высококардинальных labels;
- явный `Cancel`/`Join` без фоновых goroutine.

Не входят в пакет:

- проверка конкретной AsyncAPI message schema и доменная eligibility policy;
- извлечение authority, tenant или consumer scope из недоверенного payload;
- NATS JetStream либо иной broker consumer;
- внешний необратимый effect внутри PostgreSQL-транзакции;
- создание schema или скрытый запуск миграций;
- операторский `skip`, способный объявить неприменённый бизнесовый effect
  успешным.

## Public API

- `New(Beginner, Config, ...Option)` создаёт Processor без I/O и goroutine;
- `Process` выполняет receive → claim → effect → finalization целиком;
- `Acquire`, `Renew` и `ApplyClaim` дают тот же протокол adapter, которому
  нужен явный раздельный claim/renew flow;
- `Check` проверяет exact schema и working principal path;
- `Repair` выполняет только авторизованный audited `REQUEUE`, `Cleanup` —
  bounded retention;
- `Cancel` и `Join` задают lifecycle перед закрытием PostgreSQL;
- `WithObserver`/`WithTracer` подключают только закрытые operation/outcome;
- `WithRepairAuthorizer` подключает обязательную trusted boundary, которая
  разрешает actor из проверенного context или authoritative state.

`Config` фиксирует schema, instance identity, lease/effect/finalize budgets,
retry/backoff/attempt/repair limits, retention horizon и cleanup batch. Нулевые
`FinalizeTimeout` и `CleanupBatchSize` получают defaults `5s` и `100`;
остальные safety-параметры обязательны.

## Идентичности и неизменяемость

### Event identity и digest

`eventId` берётся из предварительно проверенного `eventing.Envelope`. Пакет
повторно вызывает `Envelope.Validate`, сериализует envelope через
`Envelope.Marshal` и вычисляет SHA-256 от всех metadata и `data`. Digest
неизменяемо сохраняется в `runtime_inbox_events.event_digest`.

Ключ dedup:

```text
(consumer_name, consumer_scope, event_id)
```

Совпавший ключ и digest означает redelivery того же факта. Совпавший ключ с
иным digest, ordering key или sequence означает `event_conflict`; такой вход
никогда не выполняет effect и никогда не получает успешный `ACK`.

### Consumer identity и scope

`consumer_name` — стабильное имя materialized consumer из AsyncAPI/deploy
registry. `consumer_scope` — назначенная composition root область одного
независимого cursor/dedup пространства, например имя проекции или устойчивое
поколение consumer contract. Оба значения приходят из доверенной
конфигурации, а не из envelope, broker header или пользовательского payload.

Изменение `consumer_name` либо `consumer_scope` создаёт новую историю и не
является способом очистить старый inbox. Такой переход требует отдельного
миграционного плана, bootstrap cursor и проверки PITR/replay horizon.

## Порядок и cursor

Ordering key является JSON-массивом строк без delimiter collision:

```text
[eventName, aggregateType, aggregateId]
[organizationId, eventName, aggregateType, aggregateId]
```

Второй вариант применяется только при непустом `organizationId`. Go-код и
PostgreSQL generated column используют одну функцию и сравниваются при каждом
receive. Cursor имеет ключ:

```text
(consumer_name, consumer_scope, ordering_key)
```

Первое допустимое значение `eventSequence` равно `1`. `last_sequence` хранит
последний атомарно применённый effect. Допустим только `last_sequence + 1`:

- равное или меньшее значение для нового `eventId` — `stale`, effect не
  выполняется, durable evidence сохраняется;
- большее значение — `gap`, cursor не меняется и broker получает решение
  повторной доставки;
- точное следующее значение может быть claim-нуто одним worker;
- `dead_letter` на точном следующем значении блокирует всех последователей.

`runtime_event_cursors` не очищается автоматически. Он сохраняет high-watermark,
последние `eventId`/digest и следующий fence независимо от cleanup строк inbox.

## Автомат состояний

```text
receive
  -> RECEIVED
  -> STALE                         (durable, ACK допустим после commit)
  -> existing COMPLETED|STALE      (duplicate, ACK допустим после read)
  -> gap|conflict|terminal         (ACK запрещён)

RECEIVED|RETRY|expired PROCESSING
  -> claim -> PROCESSING(generation, fence, lease)

PROCESSING
  -> effect + inbox COMPLETE + cursor advance -> COMPLETED
  -> retryable error -> RETRY(available_at, error_code)
  -> non-retryable / budget exhausted -> DEAD_LETTER
  -> crash -> PROCESSING до lease expiry -> новый generation/fence

DEAD_LETTER
  -> audited bounded REQUEUE repair -> RETRY

COMPLETED|STALE
  -> bounded cleanup после обоих retention gates
```

Eligibility, lease start/expiry, `available_at`, completion, terminal time и
cleanup рассчитываются по `clock_timestamp()` PostgreSQL. Время процесса не
решает, кто может claim, renew, finalize, repair или cleanup.

## Полный receive → cleanup

1. Broker adapter строго проверяет envelope и конкретную AsyncAPI schema.
2. `Processor.Process` принимает trusted `Consumer`, envelope и `Handler`.
3. Receive-транзакция создаёт либо авторитетно читает immutable inbox row,
   блокирует cursor и детерминированно классифицирует duplicate/stale/gap/
   conflict.
4. Claim-транзакция проверяет точного предшественника, backoff и terminal
   blockage, затем назначает `lease_owner`, случайный `lease_token`, новое
   `lease_generation` и монотонный cursor fence.
5. Effect-транзакция повторно блокирует inbox/cursor и сверяет event digest,
   owner, token, generation, fence, lease expiry и ожидаемую sequence.
6. Handler получает `pgx.Tx` savepoint той же физической PostgreSQL-
   транзакции. При успехе через этот же `pgx.Tx` записываются `COMPLETED` и
   cursor, затем фиксируется единственный верхнеуровневый commit.
7. При ошибке handler savepoint откатывается, поэтому частичный локальный
   effect не сохраняется. Внешняя транзакция устойчиво фиксирует `RETRY` либо
   `DEAD_LETTER`; cursor остаётся прежним.
8. Только после успешного верхнеуровневого commit пакет возвращает durable
   broker action. Ошибка commit не может вернуть `ACK`.
9. `Repair` сначала запрашивает server-resolved actor у `RepairAuthorizer`,
   затем повторно ставит только самый ранний точный dead-letter predecessor
   после проверки digest, generation/fence, idempotency request hash,
   actor/reason/evidence и repair budget. Без настроенного authorizer repair
   закрыто запрещён.
10. `Cleanup` удаляет ограниченную порцию только `COMPLETED|STALE`, для которых
    прошли сохранённый `cleanup_after` и текущий минимальный retention horizon.

Receive и claim являются отдельными явно документированными durable
переходами. Транзакция effect не открывает второй metadata commit: успешный
локальный effect, `COMPLETED` evidence и продвижение cursor существуют только
вместе. Handler не получает pool либо repository shortcut от библиотеки —
только точный `pgx.Tx`. Он обязан использовать transaction-bound adapters и
не выполнять внешний необратимый вызов.

## Владение транзакцией

`Processor` начинает транзакции через переданный `Beginner`; environment pool
и его закрытие принадлежат composition root. `Process` передаёт handler
`pgx.Tx` и сам обрабатывает `Begin`, savepoint `Begin`, `Commit`, `Rollback` и
ошибки commit. Верхнеуровневые commit/rollback получают отдельные bounded
contexts с `FinalizeTimeout`, созданные от `context.WithoutCancel` переданного
контекста; исчерпание caller context не оставляет cleanup без бюджета. Пакет не
создаёт соединение, pool, root context либо скрытую transaction для consumer
effect.

Если результат должен уйти наружу, handler записывает локальное состояние и
новое outbox-событие через transaction-bound adapter в полученном `pgx.Tx`.
Сетевой RPC, broker publish, файловый effect и иной необратимый side effect в
handler запрещены: PostgreSQL rollback не способен их отменить.

Все верхнеуровневые транзакции используют `pgx.Serializable` и
`pgx.StrictNamedArgs`. Операции без consumer effect ограниченно повторяются не
более трёх раз только для SQLSTATE `40001`/`40P01`; receive и repair также
повторяют `23505`, чтобы после конкурентной вставки перечитать авторитетное
dedup/idempotency evidence. Effect-транзакция никогда автоматически не
повторяет handler: serialization/deadlock/неопределённая ошибка commit
возвращают нулевой `Result`, а broker redelivery выполняет авторитетное
восстановление через inbox.

## Владение ACK/NACK

Закрытые действия `BrokerAction`:

| Action          | Broker adapter                                                                  |
| --------------- | ------------------------------------------------------------------------------- |
| `ack`           | Выполнить `ACK`; допустимо только для durable `processed|duplicate|stale`.       |
| `nack_retry`    | Не подтверждать; применить bounded redelivery/backoff broker contract.           |
| `nack_terminal` | Не подтверждать как успех; сохранить/сверить incident/dead-letter evidence.      |

`nack_terminal` не разрешает молчаливый `ACK`. Provider-specific `Term`
допустим только когда внешний consumer contract гарантирует устойчивое
incident/dead-letter evidence и авторитетный repair path. Сам пакет такого
решения не принимает.

При infrastructural ошибке до устойчивого решения возвращается ошибка и
нулевой `Result`; безопасный default broker adapter — `nack_retry`.
Семантический gap/conflict/dead-letter либо устойчиво записанный handler
failure могут вернуть одновременно `Durable=true` и классифицированную ошибку:
boundary логирует ошибку один раз, но выбирает broker action прежде всего по
durable `Result`. Наличие ошибки само по себе не отменяет сохранённое решение.

## Матрица отказов и восстановления

| Сценарий | Durable переход | Cursor/effect | Результат broker |
| --- | --- | --- | --- |
| Первое точное следующее событие | `RECEIVED -> PROCESSING -> COMPLETED` | effect и cursor фиксируются вместе | `ack` после commit |
| Тот же `eventId` и digest после completion | авторитетное чтение `COMPLETED` | без effect/изменения cursor | `ack` (`duplicate`) |
| Новый `eventId`, sequence уже пройдена | `STALE` с digest | без effect/изменения cursor | `ack` после commit |
| Sequence больше `last+1` | `RECEIVED`, `sequence_gap` | без effect/изменения cursor | `nack_retry` |
| Тот же `eventId` с иным содержимым | без изменения сохранённой строки | без effect/изменения cursor | `nack_terminal`, `ErrEventConflict` |
| Другой `eventId` для того же key+sequence | без изменения predecessor | без effect/изменения cursor | `nack_terminal`, `ErrEventConflict` |
| Crash до effect commit | effect-транзакция откатывается; claim истекает | без cursor/effect | redelivery, новый generation/fence |
| Crash после effect+inbox+cursor commit до ACK | строка `COMPLETED` | effect не повторяется | redelivery возвращает `duplicate/ack` |
| Broker redelivery во время действующей lease | claim не меняется | без второго effect | `nack_retry` (`busy`) |
| Replay после cleanup, sequence ниже cursor | новая `STALE` evidence | без effect | `ack` после commit; conflict гарантирован в retention horizon |
| Lease expiry до начала effect | новый claim увеличивает generation/fence | старый claim не действует | новый worker продолжает |
| Lease формально истекла во время заблокированной effect tx | row lock сохраняет победителя; finalization сверяет PostgreSQL time | просроченный finalize закрыто отклоняется и tx откатывается | `nack_retry` |
| Stale owner/token/generation/fence | изменение не выполняется | без cursor/effect | `nack_retry`, `ErrStaleClaim` |
| Retryable handler error | savepoint rollback, `RETRY` с bounded backoff | без cursor/effect | `nack_retry` |
| Attempt budget исчерпан | `DEAD_LETTER` | predecessor блокирует cursor | `nack_terminal` |
| Repair без authority | без изменения event | без cursor/effect | `ErrRepairNotAllowed` |
| Повтор repair с тем же key/hash | возвращается сохранённый receipt | без второго repair | прежний результат |
| Repair key с иным request hash | без изменения event | без cursor/effect | `ErrRepairConflict` |
| Stale repair generation/fence/digest | без изменения event | без cursor/effect | `ErrStaleClaim`/`ErrEventConflict` |
| Cleanup до safety horizon | строка не eligible | evidence сохраняется | без broker action |
| Cleanup terminal/predecessor/cursor/repair | запрещён SQL predicate/schema | evidence сохраняется | без broker action |
| Readiness marker/object mismatch | startup fail-closed | subscription не запускается | сообщения не принимаются |

## Retry, dead letter и repair

Attempt увеличивается только победившим claim. Backoff вычисляется как
ограниченная экспонента `InitialBackoff * 2^(attempt-1)` с верхней границей
`MaximumBackoff`; eligible time сохраняется относительно PostgreSQL time.
`MaxAttempts` сохраняется при первом receive, поэтому rollout config не меняет
бюджет уже принятого события.

`DEAD_LETTER` остаётся в inbox и блокирует следующий sequence. `Repair`
поддерживает только `REQUEUE`, не меняет envelope/digest/key/sequence и не
продвигает cursor. Запрос включает причину, SHA-256 evidence, expected
generation/fence и idempotency key. Actor отсутствует в request и возвращается
`RepairAuthorizer` только из проверенного context либо authoritative state;
default authorizer всегда запрещает repair. Actor входит в canonical request
digest и durable receipt. Receipt хранится в `runtime_inbox_repairs` независимо
от последующего cleanup event row. Число repair ограничено сохранённым
`max_repairs`.

## Schema и миграции

[`schema.sql`](schema.sql) — точный schema contract версии `1`. Он содержит:

- `runtime_event_schema_versions` и marker `postgresinbox`;
- immutable ordering-key function и generated column;
- `runtime_inbox_events`, `runtime_event_cursors`, `runtime_inbox_repairs`;
- точные constraints и partial indexes рабочего пути.

Библиотека никогда не выполняет этот SQL. Каждый сервис-владелец включает
эквивалентную DDL в собственную forward-only goose migration и устанавливает
marker только после создания всех объектов в той же атомарной migration.
Применённая migration не редактируется; rollback — новая компенсирующая
forward migration по `expand -> migrate -> contract`.

Schema/readiness contract v1 рассчитан на PostgreSQL 17+ и проверен по
`/websites/postgresql_current`; нижняя граница обусловлена exact проверкой
табличной привилегии `MAINTAIN`. Неподдерживаемый catalog или версия
PostgreSQL завершают readiness закрытым отказом, а не ослабленной проверкой.

`Check` использует тот же transaction beginner, `search_path`, runtime
principal и query loader, что рабочие операции. Он fail-closed сверяет marker,
required table/function, columns/types/nullability/defaults/generated
expression, table access method/RLS/rules/triggers/inheritance/replica identity,
constraints, indexes, predicates, validity и фактические privileges. Совпавшее
имя с другой сигнатурой несовместимо. Дополнительные service-owned объекты
разрешены только под другими именами и не могут менять required definition.

Рабочая транзакция устанавливает точный `search_path` вида
`pg_catalog,<service_schema>,pg_temp`: первое место `pg_catalog` запрещает
service-owned функциям затенять built-ins, а явное последнее место `pg_temp`
запрещает временной таблице затенить runtime relation. Readiness требует
`USAGE` без `CREATE` для runtime principal на service schema, проверяет, что
runtime principal не входит в роли владельцев schema/required tables/function,
и сверяет exact table privileges рабочего пути: marker `SELECT`, cursors
`SELECT|INSERT|UPDATE`, events `SELECT|INSERT|UPDATE|DELETE`, repairs
`SELECT|INSERT`; `TRUNCATE|REFERENCES|TRIGGER|MAINTAIN` запрещены. Каждая
транзакция также закрыто требует неизменную identity
`current_user = session_user`.

## Retention, PITR и cleanup

`RetentionHorizon` обязан быть не меньше:

```text
maximum producer backup/PITR horizon
+ maximum delivery/redelivery delay
+ operational safety margin
```

При completion пакет сохраняет `cleanup_after` по PostgreSQL time. Cleanup
повторно применяет текущий `RetentionHorizon`; увеличение настройки тем самым
защищает старые строки, а уменьшение не сокращает уже сохранённый срок.

Удаляются только `COMPLETED|STALE` bounded batch. Никогда автоматически не
удаляются cursor/high-watermark, repair receipts, `RECEIVED`, `PROCESSING`,
`RETRY`, `DEAD_LETTER` либо predecessor evidence. До возобновления producer
после PITR оператор сверяет restore point, consumer cursor и фактический inbox
horizon.

## Lifecycle

`Processor` не создаёт goroutine. Внешний broker adapter вызывает `Process` в
своих управляемых workers. `Cancel` атомарно прекращает приём новых вызовов;
уже принятые операции завершаются на переданном caller context. `Join(ctx)`
ожидает их без вспомогательной goroutine и закрывается по deadline вызывающей
стороны. Handler обязан соблюдать переданный `EffectTimeout` context; Go не
может принудительно остановить handler, который игнорирует cancellation.
`Join` отслеживает активные вызовы API, но не изображает durable lease
goroutine: между `Acquire` и `ApplyClaim` split-flow принадлежит внешней worker
group. После cancel новый `ApplyClaim` закрыто отклоняется, а оставленный claim
восстанавливается только после PostgreSQL lease expiry.

Порядок composition root:

1. создать PostgreSQL pool и `Processor`;
2. выполнить bounded `Processor.Check` и зарегистрировать его обязательной
   readiness dependency;
3. пройти producer relay/publisher readiness;
4. после startup barrier запустить broker subscription в общей worker group;
5. при shutdown прекратить broker fetch, вызвать `Processor.Cancel`, отменить
   caller worker contexts, дождаться внешней worker group и вызвать bounded
   `Processor.Join`;
6. только после обоих join закрыть broker и PostgreSQL pool.

## Observability

`Observer` и `Tracer` получают только значения закрытых `Operation` и
`Outcome`. No-op реализации используются по умолчанию. Payload, SQL, error
text, event/aggregate/tenant/correlation ID, ordering key, consumer name и
scope не передаются как labels/attributes.

Пакет не логирует ошибки. Он возвращает безопасно обёрнутую причину наверх, а
broker/process boundary логирует её ровно один раз. Runtime error strings
остаются английскими.

## Security

- Consumer identity/scope назначает server-side composition root.
- Envelope и его `organizationId` не являются authority для доменного effect.
- Handler до записи применяет service-owned eligibility/owner policy из
  проверенного transport/signed context либо authoritative state.
- Runtime PostgreSQL principal, TLS `verify-full`, RLS/privileges и egress
  принадлежат сервису; `Check` не заменяет deploy/security validation.
- Event payload и identifiers не попадают в diagnostics/metric labels.
- Случайный lease token, generation, fence и digest никогда не доказывают
  actor authority; они только ограждают конкурентную обработку.
- Repair request не содержит actor. `RepairAuthorizer` обязан сопоставить exact
  consumer/event/digest/generation/fence с actor из проверенного context либо
  authoritative state; без этого hook операция fail-closed.

## Минимальное подключение

```go
processor, err := postgresinbox.New(pool, postgresinbox.Config{
    Schema:           "runtime_controller",
    InstanceID:       instanceID,
    LeaseDuration:    30 * time.Second,
    EffectTimeout:    20 * time.Second,
    FinalizeTimeout:  5 * time.Second,
    InitialBackoff:   time.Second,
    MaximumBackoff:   time.Minute,
    MaxAttempts:      8,
    MaxRepairs:       3,
    RetentionHorizon: 35 * 24 * time.Hour,
    CleanupBatchSize: 100,
})
if err != nil {
    return err
}
if err := processor.Check(startupCtx); err != nil {
    return err
}

result, err := processor.Process(
    messageCtx,
    postgresinbox.Consumer{Name: "runtime-controller", Scope: "v1"},
    envelope,
    func(ctx context.Context, tx pgx.Tx, event eventing.Envelope) error {
        return projection.ApplyTx(ctx, tx, event)
    },
)
if err != nil {
    consumeBoundary.ObserveError(err) // Единственное логирование — во внешней boundary.
}

switch {
case result.Durable && result.Action == postgresinbox.BrokerActionACK:
    return message.Ack()
case result.Durable && result.Action == postgresinbox.BrokerActionNACKTerminal:
    // Adapter сверяет durable incident/dead-letter policy; успешный ACK запрещён.
    return message.Nak()
default:
    // Включает gap, retry, busy, cancel и ошибку до durable decision.
    return message.Nak()
}
```

Перед закрытием pool:

```go
processor.Cancel()
if err := processor.Join(shutdownCtx); err != nil {
    return err
}
pool.Close()
```

Пример намеренно не импортирует NATS SDK: `message.Ack/Nak` обозначают методы
конкретного broker adapter за пределами общего API.

## Совместимость и rollback

Public API и storage contract versioned вместе. Старый binary продолжает
работать только с явно совместимым marker и точными объектами schema.
Неизвестная версия либо изменённая сигнатура закрывает readiness.

Rollback binary разрешён только пока его `Check` принимает уже расширенную
schema. Несовместимый rollback не выполняется; применяется новый forward-only
compensating change. Удаление колонок, evidence или marker не используется как
оперативный откат.

## Проверенная актуальная документация

Через Context7 перед реализацией проверены:

- `/jackc/pgx` — `pgx.Tx`, `Begin`/`Commit`/`Rollback`, nested transaction как
  savepoint, `StrictNamedArgs`, `pgconn.PgError.SQLState` и границы retry;
- `/websites/postgresql_current` — `FOR UPDATE`/`SKIP LOCKED`, PostgreSQL-time
  eligibility, `READ COMMITTED`/`SERIALIZABLE`, serialization retry,
  `pg_catalog`/`information_schema`, generated columns, constraints и
  index validity/predicate inspection, `pg_has_role` и exact
  `has_table_privilege` (`SELECT|INSERT|UPDATE|DELETE|TRUNCATE|REFERENCES|`
  `TRIGGER|MAINTAIN`).
