---
id: ADR-DOC-004
title: Транзакционный outbox для доменных событий
type: adr
status: approved
owner: architect
version: 1.0.1
updated: 2026-07-28
---

# Транзакционный outbox для доменных событий

## Статус ADR

Accepted.

Решение принято как базовый архитектурный профиль MatterCodex. Замена broker
или способа размещения relay требует отдельного ADR с сохранением атомарности,
идемпотентности и recovery contract.

## Контекст

Доменное изменение и событие о нем нельзя фиксировать двумя независимыми
операциями: commit данных без события оставит потребителей с устаревшей
проекцией, а событие без commit опубликует несуществующий факт. При этом выбор
Kafka, NATS или иной шины не должен проникать в доменные сервисы и
транзакционные сценарии.

## Решение

1. PostgreSQL является транзакционным источником доменных изменений, аудита,
   результатов идемпотентных команд, последовательностей агрегатов и outbox.
2. Доменный сервис в одной serializable-транзакции сохраняет агрегат и запись
   outbox. Ошибка любой части откатывает обе записи.
3. Payload формируется по протокол-независимому AsyncAPI-контракту до записи в
   outbox. В таблице хранятся стабильные `eventId`, имя, версия схемы,
   агрегат, `eventSequence`, время и безопасный JSON payload.
4. Доменный код не вызывает Kafka, NATS, HTTP webhook или SDK брокера.
5. Физическую доставку выполняет отдельный relay-адаптер. Он читает
   неподтвержденные записи outbox, публикует их через порт шины и только после
   подтверждения отмечает доставку. Повторы допустимы, потребитель
   дедуплицирует их по `eventId`.
6. Для первого контура PostgreSQL outbox является долговечным буфером и
   источником relay. Подключение Kafka или другой шины меняет relay-адаптер и
   deployment-конфигурацию, но не use case, транзакцию записи или AsyncAPI.
7. Прямое чтение таблиц другого сервиса запрещено. До появления relay внешние
   подписчики не объявляются работающими.
8. Каждый потребитель хранит долговечный inbox с уникальным `eventId` и
   атомарно фиксирует inbox row вместе с локальным доменным effect. Retention
   inbox покрывает максимальный backup/PITR horizon producer и safety margin.
9. После восстановления producer relay возобновляется только после сверки
   restore point с consumer checkpoints/inbox horizon. Повторная доставка
   должна приводить к одному effect.
10. Producer периодически читает только агрегированную техническую сводку
    outbox с коротким deadline и сохраняет ее в Prometheus gauges. HTTP
    `/metrics` не выполняет SQL и отдает последний успешный snapshot: число
    pending/published, возраст старейшего pending, сумму попыток и число
    pending с ошибкой.
11. Отказ observer не останавливает доменные команды, но учитывается отдельной
    метрикой и alert. Relay обязан сохранять `publish_attempts`,
    `last_error_code` и `published_at`, чтобы snapshot отражал повторы,
    ошибки и восстановление без чтения payload.
12. Общая реализация находится в `libs/go/eventing`: provider-neutral relay
    использует lease, persisted exponential backoff, bounded concurrency,
    отдельный finalize budget и dead letter; PostgreSQL inbox атомарно
    фиксирует dedup, cursor и consumer effect.
13. Первый consumer не запускает broker subscription, пока producer relay,
    publisher и inbox schema не прошли обязательные startup/readiness checks.
    Замена broker реализуется новым `eventing.Publisher`.
14. Общая runtime-схема имеет атомарно устанавливаемый version marker.
    Outbox/inbox `Check` закрыто отклоняют отсутствующую или несовместимую
    версию до запуска listener и workers.
15. Ordering key вычисляется единообразно из утвержденного envelope:
    `eventName + aggregateType + aggregateId`, а контракты с
    `organizationId` добавляют его первым компонентом. Ключ хранится явно в
    outbox, inbox и cursor.
16. Eligibility, lease, persisted backoff и finalization используют часы
    PostgreSQL. Relay арендует не больше доступной локальной concurrency за
    одну волну.

## Последствия

- Нельзя потерять событие между commit бизнес-данных и постановкой на доставку.
- События доставляются как минимум один раз, поэтому идемпотентность
  потребителей обязательна.
- Порядок гарантируется только внутри утвержденного ordering key.
- Отставание relay не блокирует commit доменной команды, но требует метрик,
  алерта и runbook для backlog outbox.
- Замена транспорта не требует изменения доменных сервисов. Новая шина должна
  пройти контрактные тесты payload, подтверждения публикации, повторов и
  восстановления после сбоя.
- Общий relay, durable inbox и базовый NATS JetStream adapter реализуются в
  `libs/go/eventing`. До подключения первого межсервисного consumer проект
  обязан определить и проверить точную stream/consumer-конфигурацию, service
  migrations, startup wiring и AsyncAPI. Альтернативный broker adapter
  реализуется в `libs/go/eventing` и проходит тот же provider-neutral contract.
- TTL-кэш или broker offset без durable inbox не выполняет контракт
  дедупликации после PITR.

## Связанные документы

- `DOM-MC-001`;
- `ARCH-MC-004`;
- `SVC-DOC-001`;
- `SVC-DOC-002`;
- `GO-DOC-004`;
- `GO-DOC-005`;
- `docs/runbooks/`;
