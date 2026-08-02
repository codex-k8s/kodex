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
- `ReadDeliveryOutcome` авторитетно возвращает exact durable delivery decision
  после restart без payload/PII или claim coordinates;
- `GetBlockage`/`ListBlockages` дают авторизованный bounded operator read
  незавершённых predecessors, `Recover` фиксирует решение после исчерпания
  broker redelivery;
- `Repair` выполняет только авторизованный audited `REQUEUE`, `Cleanup` —
  bounded retention;
- `Cancel` и `Join` задают lifecycle перед закрытием PostgreSQL;
- `WithObserver`/`WithTracer` подключают только закрытые operation/outcome;
- `WithOperatorAuthorizer` подключает обязательную trusted boundary, которая
  назначает actor и canonical `(organization, project, operation, key hash)`;
- `WithEffectOperations` регистрирует закрытый набор service-owned PostgreSQL
  functions, доступных узкой `EffectTx` capability.

`Config` фиксирует schema, instance identity, lease/effect/finalize budgets,
retry/backoff/attempt/repair limits, retention horizon и cleanup batch. Нулевые
`FinalizeTimeout` и `CleanupBatchSize` получают defaults `5s` и `100`;
остальные safety-параметры обязательны.
Проверка `EffectTimeout + FinalizeTimeout < LeaseDuration` выполняется без
сложения: после положительных upper bounds сравнивается
`FinalizeTimeout < LeaseDuration - EffectTimeout`, поэтому `time.Duration`
не может переполниться. Exponential backoff удваивается только после
overflow-safe сравнения с `MaximumBackoff` и насыщается на этом пределе.

## Идентичности и неизменяемость

### Event identity и digest

`eventId` берётся из предварительно проверенного `eventing.Envelope`. Пакет
сначала делает глубокую копию полного envelope и `json.RawMessage`, затем
повторно вызывает `Envelope.Validate`, сериализует snapshot через
`Envelope.Marshal` и вычисляет SHA-256 от всех metadata и `data`. Digest
неизменяемо сохраняется в `runtime_inbox_events.event_digest`.

`Handler` получает `EventSnapshot`; каждый вызов `Data`/`Envelope` возвращает
новую копию. Мутация caller buffer после входа в `Process`, конкурентная
мутация исходного `RawMessage` и мутация handler view не меняют digest,
persisted metadata либо bytes, переданные effect.

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

broker MaxDeliver exhausted
  -> WAIT predecessor|lease|backoff
  -> REJOIN eligible row -> replay required
  -> TERMINALIZE exhausted row -> repair required

COMPLETED|STALE
  -> bounded cleanup после обоих retention gates
```

Eligibility, lease start/expiry, `available_at`, completion, terminal time и
cleanup рассчитываются по `clock_timestamp()` PostgreSQL. Время процесса не
решает, кто может claim, renew, finalize, repair или cleanup.

Все mutating flows, которым нужны обе строки, берут locks только в порядке
`cursor -> inbox`. `Recover` и `Repair` сначала неблокирующе разрешают
immutable ordering coordinates, затем блокируют cursor, после него inbox через
`FOR UPDATE` и повторно сверяют event ID/digest/ordering key/sequence и
generation/fence/state. Исчезновение либо изменение строки между read и lock
закрыто классифицируется как stale claim. `receive`, `claim` и `apply`
используют тот же порядок; `renew` и terminal-only `cleanup` берут только inbox
и после него никогда не пытаются взять cursor. `SERIALIZABLE` и обработка
`40P01` не заменяют этот порядок и не разрешают автоматический повтор effect.

## Полный receive → cleanup

1. Broker adapter строго проверяет envelope и конкретную AsyncAPI schema.
2. `Processor.Process` принимает trusted `Consumer`, envelope и `Handler`.
3. Receive-транзакция создаёт либо авторитетно читает immutable inbox row,
   блокирует cursor и детерминированно классифицирует duplicate/stale/gap/
   conflict.
4. Claim-транзакция проверяет точного предшественника, backoff и terminal
   blockage, затем назначает `lease_owner`, случайный `lease_token`, новое
   `lease_generation` и монотонный cursor fence.
5. Effect-транзакция повторно блокирует cursor/inbox и сверяет event digest,
   owner, token, generation, fence, lease expiry и ожидаемую sequence.
6. Handler получает `EffectTx` savepoint той же физической PostgreSQL-
   транзакции и immutable `EventSnapshot`. `EffectTx.Call` допускает только
   заранее зарегистрированную schema-qualified service function
   `(jsonb)->jsonb`; raw SQL, `Conn`, `Begin`, `Commit`, `Rollback`, session
   control и runtime inbox/cursor DML из handler недостижимы. При успехе через
   тот же savepoint записываются `COMPLETED` и cursor, затем фиксируется
   единственный верхнеуровневый commit.
7. При ошибке handler savepoint откатывается, поэтому частичный локальный
   effect не сохраняется. Внешняя транзакция устойчиво фиксирует `RETRY` либо
   `DEAD_LETTER`; cursor остаётся прежним.
8. Только после успешного верхнеуровневого commit пакет возвращает durable
   broker action. Ошибка commit не может вернуть `ACK`.
9. `Repair` сначала запрашивает server-resolved scope у `OperatorAuthorizer`,
   затем повторно ставит только самый ранний точный dead-letter predecessor
   после проверки digest, generation/fence, idempotency request hash,
   actor/reason/evidence и repair budget. Без настроенного authorizer repair
   закрыто запрещён.
10. `ReadDeliveryOutcome` по exact consumer/event/digest после restart читает
    сохранённые `COMPLETED|STALE` как `ack_eligible`; mismatch закрыт, effect и
    cursor не меняются. Для незавершённых состояний возвращается отдельный
    `wait_predecessor|wait_lease|wait_backoff|replay_required|repair_required`.
11. `GetBlockage`/`ListBlockages` возвращают только event/digest/sequence,
    hash ordering key, state/attempt/generation/fence/time/failure coordinates
    самого раннего predecessor. `Recover` сохраняет неизменяемый receipt для
    `WAIT|REJOIN|TERMINALIZE`; ни один исход не означает скрытый ACK/skip.
12. `Cleanup` удаляет ограниченную порцию только `COMPLETED|STALE`, для которых
    прошли сохранённый `cleanup_after` и текущий минимальный retention horizon.

Receive и claim являются отдельными явно документированными durable
переходами. Транзакция effect не открывает второй metadata commit: успешный
локальный effect, `COMPLETED` evidence и продвижение cursor существуют только
вместе. Handler не получает pool, raw `pgx.Tx` либо repository shortcut от
библиотеки — только transaction-bound allowlist зарегистрированных функций.

## Владение транзакцией

`Processor` начинает caller-visible физическую транзакцию через переданный
`Beginner`; environment pool и его закрытие принадлежат composition root.
`Process` передаёт handler узкую `EffectTx` и сам обрабатывает `Begin`,
savepoint `Begin`, `Commit`, `Rollback` и
ошибки commit. Верхнеуровневые commit/rollback получают отдельные bounded
contexts с `FinalizeTimeout`, созданные от `context.WithoutCancel` переданного
контекста; исчерпание caller context не оставляет cleanup без бюджета. Пакет не
создаёт соединение, pool, root context либо скрытую transaction для consumer
effect.

Если результат должен уйти наружу, зарегистрированная service function
записывает локальное состояние и новое outbox-событие в той же транзакции.
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
| Crash/restart после effect+inbox+cursor commit до ACK | строка `COMPLETED`; exact `ReadDeliveryOutcome` повторно даёт `ack_eligible` | effect не повторяется | adapter выполняет `ack` без generation/fence |
| Broker redelivery во время действующей lease | claim не меняется | без второго effect | `nack_retry` (`busy`) |
| Replay после cleanup, sequence ниже cursor | новая `STALE` evidence | без effect | `ack` после commit; conflict гарантирован в retention horizon |
| Lease expiry до начала effect | новый claim увеличивает generation/fence | старый claim не действует | новый worker продолжает |
| Lease формально истекла во время заблокированной effect tx | row lock сохраняет победителя; finalization сверяет PostgreSQL time | просроченный finalize закрыто отклоняется и tx откатывается | `nack_retry` |
| Stale owner/token/generation/fence | изменение не выполняется | без cursor/effect | `nack_retry`, `ErrStaleClaim` |
| Retryable handler error | savepoint rollback, `RETRY` с bounded backoff | без cursor/effect | `nack_retry` |
| Attempt budget исчерпан | `DEAD_LETTER` | predecessor блокирует cursor | `nack_terminal` |
| Broker MaxDeliver исчерпан на gap/busy/backoff | durable `WAIT` receipt; blockage остаётся в bounded list | без cursor/effect/ACK | новый scoped recovery после изменения eligibility |
| Broker MaxDeliver исчерпан на eligible row | `REJOIN`, row становится немедленно replay-eligible | без cursor/effect/ACK | `replay_required` из авторитетного source/DLQ |
| Broker MaxDeliver исчерпан вместе с attempt budget | `TERMINALIZE -> DEAD_LETTER` | predecessor продолжает блокировать | `repair_required`, ACK/skip запрещены |
| Broker MaxDeliver исчерпан после durable `COMPLETED|STALE` | read-only `ack_eligible` по exact consumer/event/digest | без мутации/effect/cursor | adapter выполняет фактический `ack` |
| Delivery read с иным digest/scope | без изменения/выдачи evidence | без мутации/effect/cursor | `ErrEventConflict`/`ErrDeliveryOutcomeNotFound` |
| Restart после `WAIT|REJOIN|TERMINALIZE` commit | receipt и blockage читаются из PostgreSQL | без повторной мутации | точный прежний operator outcome |
| Operator read без authority | без изменения/выдачи coordinates | без cursor/effect | `ErrOperatorNotAllowed` |
| Repair без authority | без изменения event | без cursor/effect | `ErrOperatorNotAllowed` |
| Повтор repair/recovery с тем же authorized scope/hash | возвращается сохранённый receipt | без второй мутации | прежний result/directive |
| Operator scope/key с иным request hash | без изменения event | без cursor/effect | `ErrOperatorConflict` |
| Stale repair generation/fence/digest | без изменения event | без cursor/effect | `ErrStaleClaim`/`ErrEventConflict` |
| Cleanup до safety horizon | строка не eligible | evidence сохраняется | без broker action |
| Cleanup terminal/predecessor/cursor/repair | запрещён SQL predicate/schema | evidence сохраняется | без broker action |
| Readiness marker/object mismatch | startup fail-closed | subscription не запускается | сообщения не принимаются |

## Retry, dead letter, recovery и repair

Attempt увеличивается только победившим claim. Backoff вычисляется как
ограниченная экспонента `InitialBackoff * 2^(attempt-1)` с верхней границей
`MaximumBackoff`; eligible time сохраняется относительно PostgreSQL time.
`MaxAttempts` сохраняется при первом receive, поэтому rollout config не меняет
бюджет уже принятого события.

`DEAD_LETTER` остаётся в inbox и блокирует следующий sequence. Авторитетные
`GetBlockage` и `ListBlockages` требуют `OperatorAuthorizer`, фильтруются exact
`Consumer` и возвращают не более 100 earliest predecessors с keyset cursor.
Payload, raw ordering key, organization/aggregate/correlation identifiers и
actor не выдаются. Для `Repair` возвращаются event/digest/sequence,
state/attempt/repair budget, cursor sequence, generation/fence, bounded failure
code и PostgreSQL timestamps. Raw ordering key заменён SHA-256 digest.

`ReadDeliveryOutcome` — отдельный авторизованный read точного
`Consumer + eventId + eventDigest`. Он не входит в blockage pagination и не
требует active generation/fence. После restart, crash после effect+commit до
ACK либо исчерпания broker `MaxDeliver` он повторно читает durable
`COMPLETED|STALE` и возвращает `ack_eligible`, `BrokerActionACK` и
`Durable=true`. Фактический ACK выполняет только broker adapter. Метод не
повторяет effect, не меняет cursor/state, не сохраняет receipt и не возвращает
payload, raw ordering key, event/tenant/aggregate coordinates сверх exact
identity запроса. Другой digest даёт `ErrEventConflict`; неизвестный
consumer/scope/event — `ErrDeliveryOutcomeNotFound`; отказ trusted
`OperatorAuthorizer` — закрытый `ErrOperatorNotAllowed`.

Для `RECEIVED|PROCESSING|RETRY|DEAD_LETTER` тот же read возвращает отличимый
`wait_predecessor|wait_lease|wait_backoff|replay_required|repair_required` и
никогда не выдаёт ACK. Adapter продолжает через `GetBlockage` и при
необходимости fenced `Recover`/`Repair`; read-only результат не terminalize-ит
row и не превращает исчерпание broker redelivery в скрытый skip.

Если broker исчерпал `MaxDeliver` на `gap|busy|retry`, adapter вызывает
`Recover` до provider-specific termination. `Recover` не подтверждает и не
пропускает событие: он сохраняет один из `wait_predecessor|wait_lease|`
`wait_backoff|replay_required|repair_required|ack_eligible`. Fenced `Recover`
может сохранить `ack_eligible`, если row завершилась до его lock; после restart
без claim coordinates тот же результат достигается через
`ReadDeliveryOutcome`. Фактический ACK по-прежнему выполняет adapter. После
изменения eligibility operator использует новую server-scoped idempotency
operation/key; точный повтор прежнего scope возвращает неизменяемое старое
решение. Replay выполняет внешний adapter из durable broker source/DLQ с
исходным envelope. Если source утрачен, строка остаётся видимой blockage и
cursor не продвигается.

`Repair`
поддерживает только `REQUEUE`, не меняет envelope/digest/key/sequence и не
продвигает cursor. Запрос включает причину, SHA-256 evidence, expected
generation/fence и caller key. Actor и canonical durable scope отсутствуют в
request и возвращаются `OperatorAuthorizer` только из проверенного context
либо authoritative state; default authorizer всегда запрещает operator API.
Authorizer хеширует caller key внутри server-assigned
`(organization, project, operation)` и возвращает только `key_hash`. В таблице
нет caller key. Actor, authorized scope/key hash и все request coordinates
входят в canonical request digest и durable receipt. Точный повтор возвращает
receipt; иной request digest в том же scope конфликтует; одинаковые caller
keys разных organization/project/operation независимы. Receipt хранится в
`runtime_inbox_repairs` независимо
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

Та же migration до marker отзывает `PUBLIC` на schema и всех runtime/effect
functions, выдаёт runtime principal `USAGE` schema, exact table grants и
`EXECUTE` только зарегистрированных `(jsonb)->jsonb` functions — всегда без
grant option. Имена ролей service-owned и потому не входят в общий DDL;
`Check` сверяет фактически обслуживающий `session_user`, не credential name из
конфигурации. Зарегистрированная effect function обязана быть единственной
точной `(jsonb)->jsonb`, `SECURITY INVOKER`, не set-returning и без function-
local configuration; разрешены только `VOLATILE PARALLEL UNSAFE` функции
`sql|plpgsql`, наследующие защищённый transaction `search_path`.

Schema/readiness contract v1 рассчитан на PostgreSQL 17+ и проверен по
`/websites/postgresql_current`; нижняя граница обусловлена exact проверкой
табличной привилегии `MAINTAIN`. Неподдерживаемый catalog или версия
PostgreSQL завершают readiness закрытым отказом, а не ослабленной проверкой.

`Check` использует тот же transaction beginner, `search_path`, runtime
principal и query loader, что рабочие операции. Он fail-closed сверяет marker,
required table/function, columns/types/nullability/defaults/generated
expression, table access method/RLS/rules/triggers/inheritance/replica identity,
constraints, indexes, predicates, validity и фактические privileges. Совпавшее
имя с другой сигнатурой несовместимо. Дополнительные columns, defaults,
generated expressions, constraints, unique/exclusion/expression/partial/
covering indexes и любые triggers/rules закрыто несовместимы: они способны
изменить обязательный INSERT/UPDATE/claim/cleanup path. Единственное расширение
v1 — performance-only B-tree index с именем `postgresinbox_ext_*`: только
обычные существующие columns, default opclass/collation/order, без expression,
predicate, INCLUDE, uniqueness, exclusion либо constraint ownership. Catalog
probe машинно доказывает этот узкий профиль; прочие service constraints
требуют новой совместимой версии общих runtime-объектов.

Рабочая транзакция устанавливает точный `search_path` вида
`pg_catalog,<service_schema>,pg_temp`: первое место `pg_catalog` запрещает
service-owned функциям затенять built-ins, а явное последнее место `pg_temp`
запрещает временной таблице затенить runtime relation. Readiness требует
`USAGE` без `CREATE` для runtime principal на service schema, проверяет, что
runtime principal не входит в роли владельцев schema/required tables/function,
и сверяет exact direct table privileges рабочего пути: marker `SELECT`, cursors
`SELECT|INSERT|UPDATE`, events `SELECT|INSERT|UPDATE|DELETE`, repairs
`SELECT|INSERT`; `TRUNCATE|REFERENCES|TRIGGER|MAINTAIN` запрещены. ACL grantor
обязан быть owner, grantee — только exact `session_user`, `PUBLIC` и
неожиданные roles запрещены, `WITH GRANT OPTION` запрещён для table/schema/
function/sequence. Runtime principal не может быть superuser/createdb/
createrole/replication/bypassrls. Exact v1 profile запрещает любую запись
`pg_auth_members`, где runtime principal является как `member`, так и `roleid`,
независимо от `inherit_option`/`set_option`: поэтому он не входит в чужую роль,
а чужая role/login не может унаследовать его DML или выполнить `SET ROLE` в
него; первый и последний edge любой транзитивной цепочки также закрыты.
Service sequences допускают только прямые non-grantable
`USAGE|SELECT` runtime principal без `PUBLIC`/третьих roles; общий contract сам
sequence не создаёт. Функция ordering key явно отзывает default `PUBLIC
EXECUTE`, а migration выдаёт exact `EXECUTE` runtime principal. Каждая
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
- Handler видит только immutable snapshot и зарегистрированные effect
  functions. Function применяет service-owned eligibility/owner policy из
  проверенного transport/signed context либо authoritative state.
- Runtime PostgreSQL principal, TLS `verify-full`, RLS/privileges и egress
  принадлежат сервису; `Check` не заменяет deploy/security validation.
- Event payload и identifiers не попадают в diagnostics/metric labels.
- Случайный lease token, generation, fence и digest никогда не доказывают
  actor authority; они только ограждают конкурентную обработку.
- Operator request не содержит actor/organization/project/operation/key hash.
  `OperatorAuthorizer` обязан сопоставить exact action/consumer/event/digest/
  scope с проверенным context либо authoritative state. Для mutation он также
  проверяет generation/fence и caller key и назначает canonical durable scope;
  read-only delivery outcome не принимает caller key или claim coordinates.
  Без этого hook delivery read/blockage/recovery/repair fail-closed.

## Минимальное подключение

```go
applyProjection, err := postgresinbox.NewEffectOperation(
    "apply_projection",
    "runtime_controller",
    "apply_projection",
)
if err != nil {
    return err
}

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
},
    postgresinbox.WithEffectOperations(applyProjection),
    postgresinbox.WithOperatorAuthorizer(operatorAuthorizer),
)
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
    func(
        ctx context.Context,
        tx postgresinbox.EffectTx,
        event postgresinbox.EventSnapshot,
    ) error {
        input, err := event.Envelope().Marshal()
        if err != nil {
            return err
        }
        _, err = tx.Call(applyProjection, input)
        return err
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

После restart либо crash после commit до broker ACK adapter сначала выполняет
точный авторизованный read и не нуждается в сохранённых generation/fence:

```go
delivery, err := processor.ReadDeliveryOutcome(operatorCtx,
    postgresinbox.DeliveryOutcomeRequest{
        Consumer:    consumer,
        EventID:     eventID,
        EventDigest: immutableDigest,
    },
)
if err != nil {
    return err
}
if delivery.Durable &&
    delivery.Directive == postgresinbox.RecoveryACKEligible &&
    delivery.Action == postgresinbox.BrokerActionACK {
    return message.Ack()
}
// pending/gap/busy/retry/dead-letter остаются NACK/operator recovery path.
return message.Nak()
```

При исчерпании broker redelivery adapter не делает скрытый success ACK:

```go
blockage, err := processor.GetBlockage(operatorCtx, consumer, eventID)
if err != nil {
    return err
}
receipt, err := processor.Recover(operatorCtx, recoveryRequestFrom(blockage))
if err != nil {
    return err
}
if receipt.Directive == postgresinbox.RecoveryReplayRequired {
    return durableBrokerSource.Replay(blockage.EventID)
}
```

`recoveryRequestFrom` передаёт exact digest/generation/fence и evidence, но не
actor/tenant authority. `OperatorAuthorizer` назначает organization/project/
operation/key hash из trusted context/authoritative state.

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
  `TRIGGER|MAINTAIN`), `has_*_privilege ... WITH GRANT OPTION`,
  `aclexplode`, `acldefault` для table/schema/function/sequence,
  `pg_auth_members` (`roleid`, `member`, `inherit_option`, `set_option`) и
  `pg_has_role` (`MEMBER|USAGE|SET`). Также проверено требование единого порядка
  row locks для предотвращения deadlock.
