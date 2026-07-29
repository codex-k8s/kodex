---
id: GO-DOC-004
title: Надежная доставка доменных событий в Go
type: guide
status: approved
owner: architect
version: 1.1.0
updated: 2026-07-28
---

# Надежная доставка доменных событий в Go

`GO-DOC-004` задает обязательный контур публикации и обработки доменных
событий. Архитектурное решение закреплено в `ADR-DOC-004`, а наблюдаемость и
жизненный цикл процесса - в `GO-DOC-001` и `GO-DOC-003`. Базовый профиль
межсервисной коммуникации и NATS JetStream задает `GO-DOC-005`.

## Гарантии и границы

- PostgreSQL producer является источником бизнесового изменения и
  transactional outbox.
- Producer фиксирует агрегат и событие в одной транзакции.
- Relay доставляет событие через provider-neutral `eventing.Publisher` как
  минимум один раз.
- SDK Kafka, NATS или другого broker не попадает в доменный код и repository.
- Consumer фиксирует inbox, cursor и локальный durable effect в одной
  транзакции.
- Порядок гарантируется для одного проверенного `ordering key`: по умолчанию
  `eventName + aggregateType + aggregateId`, а контракт с организационной
  изоляцией добавляет `organizationId` первым компонентом.
- Повтор после broker acknowledgement или PITR допустим и не должен повторять
  бизнесовый effect.
- Первый межсервисный consumer не запускается, пока relay producer и durable
  inbox consumer не прошли startup/readiness gate.

Exactly-once между PostgreSQL и внешней шиной не заявляется. Фактический
контракт - at least once delivery и exactly-once durable effect на стороне
consumer при сохраненном inbox horizon.

## Общий envelope

Источник payload - версионированная AsyncAPI message schema. Общий envelope
содержит:

| Поле               | Инвариант                                                       |
| ------------------ | --------------------------------------------------------------- |
| `eventId`          | непустой UUID, устойчивый при повторной доставке                |
| `eventName`        | каноническое имя события; baseline NATS subject совпадает с ним |
| `eventVersion`     | положительная версия бизнесового события                        |
| `schemaVersion`    | положительная версия AsyncAPI payload                           |
| `occurredAt`       | UTC-время с точностью не выше микросекунды                      |
| `aggregateType`    | стабильный тип агрегата                                         |
| `aggregateId`      | стабильный идентификатор без персональных данных                |
| `aggregateVersion` | версия агрегата после изменения                                 |
| `eventSequence`    | непрерывная последовательность exact ordering key               |
| `correlationId`    | проверенный идентификатор сквозной операции                     |
| `causationId`      | идентификатор вызвавшего события, если применимо                |
| `organizationId`   | утвержденная контрактом необязательная область порядка          |
| `traceContext`     | утвержденный контрактом необязательный объект трассировки       |
| `data`             | JSON object, соответствующий конкретной AsyncAPI message schema |

`eventing.Parse` закрыто отказывает при неизвестном или повторяющемся поле
envelope, нескольких JSON-значениях, не-object `data` и несовпадении
метаданных. Исходная строка `occurredAt` обязана быть UTC с суффиксом `Z` и
точностью не выше микросекунд: нормализация некорректной лексической формы
после разбора запрещена. Consumer сначала вызывает `eventing.Parse`, а затем
проверяет конкретную версию message schema. Удаление неизвестных полей,
частичный decode или продолжение после parser warning запрещены.

Parser не обрезает пробелы и не нормализует маршрутизационные строки.
`eventId`, `correlationId` и `causationId` принимаются только в канонической
lowercase UUID-форме с дефисами. Необязательные `causationId` и
`organizationId` либо отсутствуют, либо содержат значение: JSON `null`
закрыто отклоняется. Успешный `Parse` обязан гарантировать, что тот же payload
будет принят metadata constraints PostgreSQL.

Допустимые расширения envelope определяются утвержденным AsyncAPI и реестром.
Общая реализация поддерживает `organizationId` и `traceContext`, но их
обязательность и содержимое проверяет конкретная message schema. Сервис не
создает локальную копию parser для расширения envelope.

Producer нормализует `occurredAt` до UTC и микросекунд до одновременного
формирования payload и значения столбца PostgreSQL. Нормализация после
сериализации запрещена: `timestamptz` не хранит наносекунды, и payload перестал
бы совпадать с авторитетными метаданными outbox.

Payload, token, секреты и персональные данные не пишутся в технические логи,
Sentry breadcrumbs и metric labels. Для диагностики используются только
безопасный bounded error code, имя события, тип агрегата и технические
счетчики. Идентификаторы события, корреляции и агрегата допустимы только как
структурированные поля лога после отдельной проверки классификации данных.

## Producer и транзакционный append

Сервис-владелец создает `postgresoutbox.Store` в composition root. Repository
получает узкий интерфейс append и вызывает его в уже открытой бизнесовой
транзакции:

```go
type EventAppender interface {
    Append(context.Context, pgx.Tx, eventing.Envelope) error
}

err := pgx.BeginTxFunc(ctx, pool, pgx.TxOptions{
    IsoLevel: pgx.Serializable,
}, func(tx pgx.Tx) error {
    if err := saveAggregate(ctx, tx, aggregate); err != nil {
        return err
    }
    return outboxStore.Append(ctx, tx, event)
})
```

Нельзя публиковать событие из доменного сервиса, выполнять append после commit,
открывать вторую транзакцию только для outbox или собирать envelope в relay.
`eventId`, sequence и payload формируются до append и сохраняются без
последующей мутации.

Конфликт уникального `eventId` или aggregate sequence является закрытым
конфликтом записи. Недоступность outbox откатывает бизнесовую транзакцию.

## Relay и broker adapter

`outbox.Relay` арендует ограниченный batch, публикует события с ограниченной
параллельностью и фиксирует результат только по совпадающим owner и lease
token. Конфигурация обязана задавать:

- уникальный `InstanceID`;
- `BatchSize` и не превышающий его `MaxConcurrency`;
- `PollInterval`;
- `LeaseDuration`;
- `PublishTimeout` и отдельный `FinalizeTimeout`;
- `MaxPublishAttempts`;
- `InitialBackoff` и `MaxBackoff`.

Должно выполняться:

```text
PublishTimeout + FinalizeTimeout < LeaseDuration
```

Publish использует lifecycle context. Фиксация `published`, retry или
dead-letter получает отдельный bounded context, который не отменяется вместе с
уже завершившимся turn публикации. Это не разрешает бесконечный shutdown:
`FinalizeTimeout` всегда ограничен.

Relay арендует за один шаг не больше `MaxConcurrency` записей. Следующая волна
claim выполняется только после publish/finalize предыдущей, поэтому
арендованная запись не ожидает локальный semaphore. Eligibility, начало lease,
expiry, finalization и `available_at` рассчитываются только по
`clock_timestamp()` PostgreSQL; процесс передает duration, но не собственное
время.

Broker adapter реализует только:

```go
type Publisher interface {
    Publish(context.Context, eventing.Envelope) error
    Check(context.Context) error
}
```

Adapter обязан дождаться подтверждения broker, классифицировать ошибку в
bounded code и признак retryability, не менять payload и не скрывать
неподтвержденную публикацию как успех. Замена broker меняет adapter и
deployment-конфигурацию, но не producer repository, доменный сервис и
AsyncAPI.

Базовая реализация находится в `libs/go/eventing/natsjetstream`. Она публикует
неизмененный serialized envelope в subject, равный `eventName`, передает
`eventId` как `Nats-Msg-Id`, отключает скрытый SDK retry внутри одного relay
attempt и ждет synchronous JetStream acknowledgement ожидаемого stream.
Adapter не создает stream: `Check` сверяет его exact environment-owned
конфигурацию. Детальный NATS contract, durable consumer и правила subjects
описаны в `GO-DOC-005`.

Broker acknowledgement имеет явный durability contract. Если broker
подтверждает запись до `fsync` или допускает потерю acknowledged messages в
настроенном окне, producer выбирает одно из решений:

- durable acknowledgement включает синхронизацию устойчивого storage;
- outbox не закрывается до дополнительного подтвержденного reconciliation;
- принятое RPO и способ повторной доставки оформлены отдельным решением.

Нельзя пометить outbox row опубликованной навсегда на основании volatile ack,
если восстановление broker способно удалить подтвержденное сообщение, а
consumer не имеет другого авторитетного пути. Replication без проверки
семантики `fsync` само по себе не доказывает durability.

## Ordering, retry и dead letter

Relay не арендует следующее событие ключа порядка, пока предыдущее не
опубликовано. Backoff хранится в PostgreSQL через `available_at`, поэтому
restart процесса не сбрасывает расписание retry.

Ключ хранится как каноническая JSON-последовательность строк без
delimiter-collision:

```text
["eventName","AggregateType","aggregate-id"]
["organization-id","eventName","AggregateType","aggregate-id"]
```

Второй вариант используется только при наличии утвержденного
`organizationId`. Один и тот же ключ вычисляется parser, outbox, inbox и
cursor.

`eventSequence` принадлежит ровно этому ordering key. Общая версия агрегата не
может использоваться как sequence отдельного `eventName`, если изменения
других событий создают в нем разрывы. `aggregateVersion` и `eventSequence`
резервируются атомарно с aggregate change и outbox append, но выражают разные
инварианты.

Неповторяемая ошибка или исчерпание попыток переводит событие в dead letter.
Dead-letter predecessor продолжает блокировать следующие события того же
ключа. Автоматически пропустить его, изменить sequence или пометить
опубликованным запрещено.

Повторная постановка выполняется только явной служебной командой сервиса через
`postgresoutbox.Store.RequeueDeadLetter` после:

1. устранения причины;
2. проверки исходного event и ожидаемой schema без публикации payload;
3. оценки последующих событий того же ordering key;
4. фиксации оператора, времени, причины и evidence;
5. проверки backlog после повторной доставки.

## Consumer и durable inbox

Consumer передает каждое проверенное событие в
`postgresinbox.Processor.Process`. Handler получает ту же `pgx.Tx`, в которой
фиксируются inbox и cursor:

```go
result, err := inbox.Process(
    ctx,
    "search-projection",
    event,
    func(ctx context.Context, tx pgx.Tx, event eventing.Envelope) error {
        return projection.Apply(ctx, tx, event)
    },
)
```

Устойчивые исходы:

- `processed` - effect, inbox и cursor зафиксированы;
- `duplicate` - тот же `eventId` и тот же payload уже обработаны;
- `stale` - новый `eventId` относится к уже пройденной sequence и effect не
  выполняется.

Повторное использование `eventId` с иным payload или metadata закрывается
`ErrEventConflict`. Пропуск sequence закрывается `ErrSequenceGap`: consumer не
подтверждает сообщение broker и не перескакивает отсутствующее событие.

Handler не выполняет необратимый внешний вызов внутри PostgreSQL-транзакции.
Если результат нужно отправить наружу, handler записывает локальное состояние
и новое outbox-событие в той же транзакции.

## Несколько event streams одного агрегата

События с разными ordering keys могут доставляться параллельно и
переставляться между потоками. Если несколько `eventName` используют одну
глобальную `aggregateVersion` для одной проекции, контракт выбирает ровно одну
merge-модель:

1. каждое событие несет полный безопасный snapshot, а consumer атомарно
   заменяет всю проекцию вместе с inbox/effect и продвижением максимальной
   `aggregateVersion`;
2. payload содержит независимые field-level versions, а consumer выполняет
   явно описанный merge каждого поля.

Частичное действие с продвижением общей версии запрещено: более новое событие
одного потока может зафиксировать version, после чего нужное изменение другого
потока будет отброшено как stale.

Lifecycle/eligibility входит в каждый snapshot, способный создать или обновить
публичную проекцию. Старое событие не может вернуть terminal, скрытый или
отозванный ресурс. Неизвестное состояние закрыто отклоняется.

AsyncAPI для каждого state-changing события фиксирует:

- полный набор consumers;
- точное действие `replace`, `merge`, `delete` или отсутствие события с
  авторитетным read path;
- version/cursor, который продвигается вместе с effect;
- поведение при duplicate, stale, gap и межпоточной перестановке.

Вычисляемое поле, полнота или дочерняя коллекция, изменяемая отдельным
событием, должна обновлять каждого consumer, который хранит соответствующую
проекцию. Случайное последующее событие не является механизмом согласования.

## Обязательный startup gate

Composition root до запуска broker subscription:

1. создает producer relay и consumer inbox;
2. выполняет bounded `relay.Check(ctx)`;
3. выполняет bounded `inbox.Check(ctx)`;
4. регистрирует обе проверки как обязательные readiness dependencies;
5. запускает relay в управляемой `serviceruntime/lifecycle.Workers` группе;
6. только после успешного startup barrier запускает broker subscription.

Если relay, publisher, inbox schema или PostgreSQL недоступны, consumer не
получает и не подтверждает сообщения. Readiness остается отрицательной. Нельзя
делать relay или inbox `OptionalForOverall` для сервиса, который публикует или
потребляет межсервисные события.

Broker callback не создает неучтенную goroutine. Subscription принадлежит
worker group, прекращает прием до закрытия PostgreSQL и ограниченно ожидается
при shutdown.

## Схема и миграции

Каждый сервис-владелец включает в собственную forward-only goose migration:

| Таблица                         | Назначение                                       |
| ------------------------------- | ------------------------------------------------ |
| `runtime_event_schema_versions` | совместимая версия общей runtime-схемы           |
| `runtime_outbox_events`         | payload, lease, retry и terminal delivery state  |
| `runtime_inbox_events`          | durable dedup и digest обработанного payload     |
| `runtime_event_cursors`         | последовательность каждого consumer/ordering key |

Обязательные индексы:

- partial claim index по `available_at, occurred_at, event_id`;
- partial ordering index по event/aggregate/sequence;
- partial published-retention index;
- partial dead-letter index;
- inbox retention index по `processed_at, consumer_name, event_id`.

Production-пример forward-only миграций находится в
`services/internal/<service>/cmd/cli/migrations`. Общая библиотека не
применяет миграции и не изменяет схему скрыто.

Публичные `Store.Check` и `Processor.Check` проверяют не только имена таблиц и
совместимую запись `runtime_event_schema_versions`, но и точные сигнатуры
колонок, тип и нормализованное определение каждого обязательного runtime
constraint, структуру, predicate и validity каждого обязательного index, а
также выражение generated ordering key. Одинаковое имя обязательного объекта с
другим определением считается несовместимой схемой. Сервис вправе добавлять
собственные constraints и indexes под отдельными именами, не изменяя
определения общих runtime-объектов. Version marker добавляется в той же
атомарной forward-only миграции, что и полностью готовые таблицы, constraints и
indexes. Ставить marker до готовности схемы запрещено.

## Retention, backup и PITR

Inbox cleanup выполняется bounded batch через `Processor.Cleanup`. Cutoff
должен быть старше:

```text
максимальный producer backup/PITR horizon + максимальная задержка доставки
+ операционный safety margin
```

Удалять inbox раньше этого горизонта запрещено: после восстановления producer
старое событие сможет повторить бизнесовый effect.

Published outbox очищается отдельной сервисной политикой только после
подтвержденного backup и окна расследования. Pending и dead-letter записи
автоматически retention не удаляет.

Перед возобновлением relay после PITR SRE сверяет producer restore point,
consumer inbox horizon и cursor. Процедура приведена в `RUN-DOC-012`.

## Наблюдаемость

`eventmetrics` регистрирует только bounded series:

- циклы relay и число claim;
- исход публикации `published|retry|dead_letter`;
- исход consumer `processed|duplicate|stale|error`;
- bounded snapshot pending, published, attempts и dead letters.

Metric labels не содержат event name, aggregate ID, event ID, topic,
correlation ID или текст ошибки. Service-specific бизнесовые метрики
размещаются в `internal/observability/metrics`.

Relay, broker subscription и inbox используют общие tracing/logging/error
boundaries. Одна ошибка не должна одновременно логироваться в repository,
relay и transport. Нижний слой возвращает безопасную классифицированную
ошибку, а единая граница записывает ее один раз.

## Проверки

Профиль определяется `GOV-DOC-003`. До первого consumer обязательны проверки
schema readiness, publish/ack failure, duplicate, gap, stale event, retry,
dead letter, restart и PITR horizon. Broker adapter проверяется через общий
provider-neutral contract, а не только happy path конкретного SDK.

## Запрещенные упрощения

- in-memory dedup, Redis TTL или broker offset вместо durable inbox;
- publish после commit бизнесовых данных;
- broker SDK в домене;
- бесконечный retry без dead letter и bounded backoff;
- ack до commit consumer effect;
- использование aggregate version как sequence несовпадающего ordering key;
- частичный projection update с продвижением общей aggregate version;
- volatile broker acknowledgement без принятого durability/recovery contract;
- ручное изменение payload, sequence, `published_at` или cursor;
- SQL из Prometheus scrape handler;
- readiness, которая не учитывает обязательный relay/inbox;
- очистка inbox без backup/PITR horizon;
- собственная копия event envelope, relay или inbox внутри сервиса.

Связанные документы: `ADR-DOC-004`, `GO-DOC-001`, `GO-DOC-002`,
`GO-DOC-003`, `GO-DOC-005`, `GUIDE-DOC-003`, `RUN-DOC-012`.
