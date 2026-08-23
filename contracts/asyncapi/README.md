---
id: CONTRACT-DOC-003
title: AsyncAPI, доменные события и NATS
type: contract-guide
status: approved
owner: architect
version: 1.1.0
updated: 2026-07-28
---

# AsyncAPI, доменные события и NATS

Исходные контракты располагаются по пути:

```text
contracts/asyncapi/<owner>/v<major>/asyncapi.yaml
```

Используется AsyncAPI 3.0. Contract описывает бизнесовый факт и consumer
semantics, а не только JSON schema.

## Channel и NATS subject

`channel.address` совпадает с каноническим `eventName`:

```yaml
channels:
  recordCreated:
    address: example.record_created
```

Имя имеет форму `<bounded-context>.<fact>` либо
`<bounded-context>.<aggregate>.<fact>`. Tenant, environment, aggregate ID,
actor и PII в subject не включаются.

Одно `eventName` навсегда связано с точными `eventVersion` и `schemaVersion`.
Несовместимое событие получает новое имя или новый major package по
утвержденному migration plan.

## Canonical envelope

Каждое state-changing event содержит:

```text
eventId
eventName
eventVersion
schemaVersion
occurredAt
aggregateType
aggregateId
aggregateVersion
eventSequence
correlationId
causationId?
organizationId?
traceContext?
data
```

- `eventId` глобально уникален и используется как broker dedup ID.
- `occurredAt` задается владельцем факта в UTC.
- `aggregateVersion` относится к состоянию aggregate.
- `eventSequence` монотонна в точном ordering key.
- `data` является immutable безопасным snapshot либо явно описанной delta.
- Payload не содержит secrets и несанкционированные PII.

Optional metadata разрешено только утвержденными extensions. Произвольные
additional properties запрещены.

## Operation semantics

Каждая publish operation фиксирует:

```yaml
x-publisher: example-service
x-consumers:
  - service: example-projection
    purpose: REPLACE_RECORD_PROJECTION
x-delivery:
  mode: AT_LEAST_ONCE
  deduplicationKey: eventId
  orderingKey: eventName+aggregateType+aggregateId
  sequenceField: eventSequence
```

Для каждого consumer дополнительно указываются:

- exact effect;
- authoritative ownership;
- eligibility/lifecycle policy;
- merge/replace/delete behavior;
- duplicate, stale и gap behavior;
- transaction, в которой фиксируются effect, inbox и cursor;
- внешний read path, если payload недостаточен.

Список consumers является частью контракта. «Любой сервис может подписаться»
не считается утвержденным ownership.

## Ordering и проекции

Базовый ordering key:

```text
[eventName, aggregateType, aggregateId]
```

При утвержденной организационной изоляции:

```text
[organizationId, eventName, aggregateType, aggregateId]
```

Разные keys обрабатываются параллельно и могут переставляться. Если несколько
event streams обновляют одну projection, контракт выбирает одну merge model:

1. полный snapshot и атомарная замена по aggregate version;
2. field-level versions и явный merge.

Частичный update с продвижением общей версии запрещен.

## Producer

Producer:

1. строит и валидирует canonical envelope до append;
2. атомарно сохраняет aggregate, audit/receipt и outbox row;
3. после commit передает доставку общему relay;
4. не импортирует NATS SDK в domain service.

NATS publisher передает `eventId` как `Nats-Msg-Id`, публикует исходный payload
в subject `eventName` и ждет acknowledgement ожидаемого stream.

## Consumer

Consumer:

1. проверяет channel, event/schema version и envelope;
2. вычисляет тот же ordering key;
3. вызывает durable inbox processor;
4. фиксирует local effect, inbox row и cursor одной PostgreSQL transaction;
5. подтверждает JetStream message только после commit.

Duplicate/stale не повторяют effect. Sequence gap и conflicting payload
закрываются ошибкой и не подтверждаются как успех.

## Запрещено

- event без publisher или consumers;
- command/request под видом события;
- dynamic subject из payload;
- direct NATS publish из handler;
- ack до durable effect;
- broker offset вместо inbox;
- aggregate ID или event name как Prometheus label;
- изменение payload в relay;
- использование aggregate version как sequence другого ordering key.

Подробные runtime-инварианты задают `GO-DOC-004` и `GO-DOC-005`.

WebSocket snapshot contract `control-api-gateway` находится в
[`control-api-gateway/v1/asyncapi.yaml`](control-api-gateway/v1/asyncapi.yaml).
Он использует `wss`, а не broker: consumer заменяет channel snapshot, а
gateway каждый раз читает авторитетное состояние через защищённый
`control-plane` RPC. Модели генерируются командой
`make gen-control-api-gateway-asyncapi`.

Для этого contract каждый используемый envelope, payload и closed enum обязан
быть named component. Имена generated Go/TypeScript models выводятся только из
source component, а не из порядка обхода schema. Актуальный
`@asyncapi/parser` сначала валидирует весь документ, затем небольшой
repository-owned generator закрыто принимает только local refs, string enums,
closed object schemas, bounded arrays и простые scalar fields. Неизвестная
schema shape завершает generation ошибкой до изменения output. Generator
воспроизводимо пересоздаёт только принадлежащие ему generated directories;
fail-only check запрещает anonymous symbols, потерю wire-name `type` и
generated JSON codecs. Ручные правки generated files запрещены.

Generated models служат воспроизводимым structural contract. Строгая JSON
граница расположена вне generated directory: входные closed enums и все
external projection enums проверяются до использования или отправки;
unknown/empty/null и out-of-range значения закрыто отклоняются. Канонический
тип projection задаёт OpenAPI, поэтому WebSocket не копирует доменную модель и
не сериализует внутренние Proto oneof/enum names.
