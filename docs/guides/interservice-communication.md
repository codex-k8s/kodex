---
id: GO-DOC-005
title: Межсервисная коммуникация и доменные события
type: guide
status: approved
owner: architect
version: 1.2.1
updated: 2026-08-23
---

# Межсервисная коммуникация и доменные события

`GO-DOC-005` задает единый способ взаимодействия deployable-компонентов.
Устройство Go-сервиса определяет `GO-DOC-001`, transactional outbox и durable
inbox - `GO-DOC-004`, а transport security - `GUIDE-DOC-003`.

## Базовые решения

| Потребность                                   | Канонический механизм                                   |
| --------------------------------------------- | ------------------------------------------------------- |
| немедленный типизированный ответ              | Proto/gRPC                                              |
| внешнее HTTP API                              | OpenAPI в gateway с вызовом внутренних gRPC             |
| внешнее двунаправленное соединение            | WebSocket в gateway                                     |
| уведомление об уже совершенном доменном факте | AsyncAPI + domain event                                 |
| надежный запуск длительной работы             | устойчивая task + domain event                          |
| периодический запуск                          | отдельная job/scheduler с устойчивой фиксацией attempt  |
| доставка события                              | PostgreSQL outbox -> relay -> NATS JetStream            |
| обработка события                             | NATS durable consumer -> PostgreSQL inbox/cursor/effect |

Сервис является единственным владельцем своего бизнесового состояния. Другой
компонент не читает его таблицы, Redis keys, S3 prefix или внутренние очереди.
Интеграция выполняется только через утвержденный контракт.

## Выбор синхронного или асинхронного пути

gRPC используется, когда вызывающему коду нужен результат до продолжения
сценария: авторизация, чтение authoritative state, validation или короткая
идемпотентная command.

Domain event используется, когда:

- факт уже зафиксирован владельцем данных;
- потребители могут обработать его независимо;
- временная недоступность потребителя не должна откатить producer transaction;
- producer не должен знать список реализаций потребителей;
- допускается eventual consistency с явно заданным SLA.

Синхронный RPC нельзя использовать как скрытую распределенную транзакцию.
Удерживать PostgreSQL transaction открытой во время сетевого вызова по
умолчанию запрещено. Если command требует данных другого сервиса, выбирается
один из вариантов:

1. получить authoritative snapshot до локальной transaction и повторно
   проверить его version/expiry при commit;
2. хранить локальную projection, обновляемую событиями;
3. зафиксировать локальную task/saga и продолжить асинхронно.

Выбранная модель фиксируется в карте сквозного сценария.

## Синхронный gRPC

### Server path

```text
mTLS peer
-> correlation interceptor
-> authentication interceptor
-> signed context verifier
-> exact RPC permission binding
-> replay reservation where required
-> validation
-> service handler
-> domain service
-> domain error mapping
-> response
```

Сервис регистрирует generated server из исходного Proto-контракта. Handler
остается тонким и не обращается к adapters напрямую.

### Client path

```text
domain service
-> narrow client port
-> service-specific client adapter
-> generated gRPC client
-> mTLS + signed authorization context
-> downstream service
```

Client adapter владеет transport credentials, timeout, retry policy и
отображением ошибок. Домен не импортирует generated client.

### Authority

Поля `actorId`, `tenantId`, `organizationId`, `ownerId`, `permission` и
provider connection из request payload не являются доказательством
полномочий. Authority выводится из:

- проверенного transport peer;
- подписанного authorization context;
- exact permission binding конкретного RPC;
- authoritative state сервиса-владельца.

mTLS подтверждает workload, но не заменяет application authorization.
Подписанный context не заменяет проверку владения конкретным aggregate.

### Deadlines, retry и ошибки

- Каждый RPC имеет bounded deadline.
- Автоматический retry разрешен только для read-only или доказанно
  идемпотентной операции.
- `Unavailable` после отправки command означает unknown outcome: client
  повторяет запрос только с тем же idempotency key.
- Transport status преобразуется в устойчивую ошибку вызывающего домена.
- Payload, metadata credentials и provider diagnostics не логируются.
- Correlation/trace context передается, но не используется как metric label.

## Domain event

Domain event описывает совершившийся бизнесовый факт в прошедшем времени.
Команда `CreateRecord` не является событием; событие называется
`record.created`, `record.status_changed` или эквивалентно утвержденной
терминологии домена.

Для каждого события AsyncAPI фиксирует:

- владельца и источник бизнесового факта;
- условие публикации;
- точную cardinality;
- payload и его compatibility policy;
- aggregate type, ID, version и event sequence;
- ordering key;
- всех потребителей и их effect;
- duplicate, stale, gap и retry behavior;
- правила удаления, terminal state и восстановления.

Событие не используется как произвольный transport DTO. Payload содержит
достаточный immutable snapshot для заявленного consumer effect либо устойчивые
идентификаторы и версии для явного authoritative read path.

Ссылочное событие с aggregate ID/version считается полным, только если каждый
заявленный consumer имеет достижимый version-pinned read/rejoin path:

```text
AsyncAPI operation
-> exact durable consumer/inbox/cursor
-> generated client operation profile
-> workload/SPIFFE/audience/method/permission binding
-> protected gRPC read exact version
-> hidden tenant semantics
-> consumer effect + inbox + cursor commit
```

Readiness до subscription проверяет этот же рабочий путь. Пропущенное событие,
restart и rejoin повторяют authoritative read; прямое чтение чужой БД
запрещено. Если такого пути нет, событие несёт полный безопасный immutable
snapshot либо consumer удаляется из контракта.

## Producer

Типовой state-changing путь:

```text
domain command
-> repository transaction
-> aggregate change
-> audit/idempotency result
-> immutable event payload
-> canonical envelope
-> runtime_outbox_events
-> one PostgreSQL commit
```

После commit relay независимо доставляет сохраненный envelope. Domain service
не импортирует NATS SDK и не публикует в broker. Ошибка outbox append откатывает
бизнесовую transaction.

Сервис использует общий `eventing.Envelope`, `postgresoutbox.Store` и
`outbox.Relay` из `libs/go/eventing`. Собственная реализация envelope, lease,
retry или dead letter запрещена.

## NATS JetStream

NATS JetStream является базовым broker adapter. Provider-neutral API позволяет
заменить его без изменения domain service, repository transaction, outbox
schema или AsyncAPI.

### Publisher

Общий adapter `libs/go/eventing/natsjetstream`:

- использует актуальный `github.com/nats-io/nats.go/jetstream`;
- публикует исходный сериализованный envelope без преобразования;
- ожидает synchronous JetStream acknowledgement;
- передает `eventId` как `Nats-Msg-Id`;
- задает expected stream при publish;
- запрещает SDK retry внутри одного relay attempt;
- проверяет непустой acknowledgement, ожидаемый stream и sequence;
- возвращает только bounded failure code и retryability;
- не логирует payload и provider diagnostics.

Outbox row помечается опубликованной только после подтвержденного ack.
Finalize сохраняет bounded broker stream/sequence/duplicate receipt и cleanup
deadline; немедленное удаление evidence запрещено. Сбой после ack, но до
PostgreSQL finalize приводит к повторной публикации; это нормальная часть
at-least-once доставки.

### Stream ownership

Broker adapter не создает и не изменяет stream. Stream является
environment-owned инфраструктурным ресурсом и доставляется code-first.
Приложение на startup/readiness сверяет фактическую конфигурацию с exact
expectation:

- name и точный список subjects;
- retention и storage policy;
- replicas;
- message/bytes/age limits;
- maximum message size;
- duplicate window;
- discard policy;
- запрет неожиданных mirror/source/republish/subject transform;
- delete/purge policy.

Production-профиль использует file storage, TLS, credentials из runtime files
и не менее трех replicas, если кластер NATS имеет достаточное число узлов.
Иное число replicas, retention или RPO фиксируется ADR.

AsyncAPI перечисляет каждый поддерживаемый environment server, включая
staging и production, с exact TLS/credential/stream profile. Отсутствующий
server не заменяется подразумеваемой общей шиной.

### Subjects

Subject совпадает с каноническим `eventName`, является частью AsyncAPI и не
строится из произвольного runtime input. Базовая форма:

```text
<bounded-context>.<fact>
<bounded-context>.<aggregate>.<fact>
```

Например:

```text
accounts.profile_created
accounts.profile.status_changed
```

Сегменты используют lowercase ASCII и стабильную утвержденную терминологию.
Одно `eventName` навсегда связано с утвержденными `eventVersion` и
`schemaVersion`. Несовместимый формат получает новое имя события; версия не
добавляется в subject автоматически. Tenant, actor, aggregate ID, environment
и персональные данные в subject не попадают. Environment разделяется отдельным
NATS account/cluster либо утвержденным инфраструктурным namespace, а не
динамическим payload-derived subject.

### Consumer

Consumer с локальным долговечным эффектом использует durable pull consumer и
explicit acknowledgement:

```text
JetStream message
-> envelope/schema verification
-> authorization/eligibility checks where applicable
-> postgresinbox.Processor.Process
-> consumer effect + inbox row + cursor in one transaction
-> commit
-> Ack
```

До успешного PostgreSQL commit сообщение не подтверждается. Retryable ошибка
приводит к `Nak`/redelivery по bounded backoff. Невалидный envelope,
несовместимая schema или конфликт payload не подтверждается молча: создается
incident/dead-letter evidence по утвержденной consumer policy.

Consumer config задает:

- exact stream и filter subjects;
- устойчивое имя durable consumer;
- explicit ack policy;
- `AckWait`;
- bounded `MaxDeliver`;
- backoff;
- maximum in-flight/fetch batch;
- delivery policy;
- replay policy.

Исчерпание `MaxDeliver` не считается успешной обработкой. Операционная политика
обязана сохранить сообщение или ссылку на него для расследования и повторного
запуска. Broker redelivery не заменяет durable inbox.

Единственное исключение для stateless owner-facing WebSocket fan-out допустимо,
когда NATS-сообщение служит только bounded wake-сигналом и не создаёт локального
бизнес-эффекта. Такой gateway не объявляет durable inbox или projection. Он
после owner authorization читает события по sequence из авторитетного event
store, периодически сверяет server-owned cursor, автоматически выполняет
catch-up/resync после пропущенного сигнала и отдаёт browser только безопасные
данные. AsyncAPI явно фиксирует `inbox: NONE_NO_LOCAL_DURABLE_EFFECT`, точный
read path и механизм восстановления. Эта граница не разрешает volatile
обработку события, создающего локальное состояние или внешний эффект.

Машинный реестр перечисляет только фактически материализованные consumer
событий. gRPC caller/producer profile не делает workload потребителем события.
Для
каждого effectful consumer совпадают AsyncAPI operation/subject, deploy owner,
effect, durable inbox/cursor, authority/read path и readiness. Для stateless
fan-out совпадают явно отсутствующий local effect/inbox, авторитетный read path
и восстановление пропусков; лишняя запись registry
считается ложной topology и закрыто отклоняется проверкой.

## Гарантии доставки

Система дает:

- at-least-once publish и delivery;
- устойчивый порядок в пределах утвержденного ordering key;
- exactly-once durable consumer effect при корректном использовании inbox;
- обнаружение duplicate, stale, gap и conflicting event;
- bounded retry и явный dead letter.

Система не обещает exactly-once между PostgreSQL и внешним broker. Нельзя
заявлять глобальный порядок между независимыми ordering keys или синхронную
консистентность всех projections.

## Startup, readiness и shutdown

Producer:

1. проверяет PostgreSQL schema и outbox;
2. создает NATS publisher;
3. сверяет exact stream expectation;
4. регистрирует outbox и publisher checks как обязательные;
5. после startup barrier запускает relay.

Consumer:

1. проверяет inbox/cursor schema;
2. проверяет stream и durable consumer contract;
3. регистрирует checks как обязательные;
4. запускает subscription только после producer relay/inbox readiness;
5. при shutdown прекращает fetch, завершает in-flight transaction, затем
   закрывает PostgreSQL и NATS connection.

Нельзя принимать сообщения при неготовом inbox или подтверждать их после
потери возможности зафиксировать effect.

## Запрещенные связи

- прямое чтение БД, Redis или S3 другого сервиса;
- HTTP между внутренними Go-сервисами без отдельного ADR;
- generated DTO в domain service;
- broker SDK в domain/repository port;
- NATS publish из handler или после commit без outbox;
- in-memory/Redis dedup вместо PostgreSQL inbox;
- доверие owner/tenant из payload;
- retry неидемпотентной command с новым idempotency key;
- ack до фиксации consumer effect;
- динамические subjects из пользовательских данных;
- создание или исправление stream приложением на startup;
- wildcard consumer без утвержденной AsyncAPI ownership.

## Проверенная документация

При подготовке документа проверены:

- NATS Go JetStream API:
  `https://github.com/nats-io/nats.go/tree/main/jetstream`;
- pgx transactions:
  `https://github.com/jackc/pgx`;
- gRPC-Go interceptors, health и graceful stop:
  `https://github.com/grpc/grpc-go`.

Перед изменением версий или API документация повторно проверяется через
Context7.

Связанные документы: `GO-DOC-001`, `GO-DOC-004`, `GUIDE-DOC-003`,
`GUIDE-DOC-006`, `GUIDE-MC-007`.
