---
id: GO-DOC-006
title: Общие библиотеки Go
type: guide
status: approved
owner: architect
version: 1.0.1
updated: 2026-07-28
---

# Общие библиотеки Go

`GO-DOC-006` определяет, какой код принадлежит `libs/go`, а какой остается в
deployable-компоненте. Структуру сервиса задают `GO-DOC-001` и
`REPO-DOC-001`, наблюдаемость - `GO-DOC-003`, события и NATS -
`GO-DOC-004`/`GO-DOC-005`.

## Критерий вынесения

Код сразу размещается в `libs/go`, если одновременно выполняется одно из
условий:

1. уже существуют два независимых production-потребителя;
2. утверждено, что это обязательный runtime/security/data primitive для
   нескольких будущих компонентов;
3. локальная реализация нарушит единый wire/storage/observability contract.

Не ждать второго потребителя для:

- gRPC error/recovery/correlation boundary;
- lifecycle/readiness/shutdown;
- telemetry runtime;
- cache engine и protobuf codec;
- event envelope, outbox relay, NATS publisher и durable inbox;
- internal RPC authentication и replay protection;
- named SQL loader;
- безопасных token primitives;
- PostgreSQL credential lifecycle.

Бизнесовые entities, permissions, event payloads, cache keys, TTL, SQL,
provider policy и use cases остаются в сервисе.

## Границы модулей

Каждая директория первого уровня `libs/go/<module>` является отдельным Go
module:

```text
libs/go/<module>/
├── go.mod
├── go.sum
├── README.md
└── *.go
```

Модули не объединяются в скрытый общий `libs/go/go.mod`. Deployable импортирует
только реально используемые библиотеки и фиксирует их через workspace/release
version. Циклические зависимости между библиотеками запрещены.

Каждый модуль документирует:

- purpose и non-goals;
- public API;
- guarantees;
- failure policy;
- lifecycle ownership;
- observability hooks;
- security boundary;
- migration/compatibility policy.

## Каталог обязательных библиотек

### `serviceruntime`

Общий process runtime:

- типизированная загрузка/валидация конфигурации;
- readiness/liveness state;
- startup barrier;
- управляемая worker group;
- cancel/join;
- bounded independent shutdown operations.

Библиотека не знает service-specific dependencies и не вызывает
`context.Background()`. Composition root передает lifecycle и базовый shutdown
context явно.

### `grpcserver`

Общая gRPC boundary:

- mTLS server options;
- unary/stream interceptors;
- correlation ID normalization;
- validation;
- panic recovery;
- trace/metrics hooks;
- единая классификация ожидаемых и неожиданных gRPC codes;
- безопасная error observer boundary.

Service-specific mapping domain errors остается в
`internal/transport/grpc/errors.go`. Библиотека не импортирует доменные пакеты.

### `httpserver`

Общий технический HTTP runtime:

- `/live`, `/ready` и `/metrics`;
- server/read/write/idle/header timeouts;
- connection limits;
- bounded graceful shutdown;
- безопасные стандартные headers.

Бизнесовые HTTP/WebSocket endpoints принадлежат gateway и не добавляются в
технический server внутреннего сервиса.

### `observability`

Provider-neutral telemetry runtime:

- отдельный Prometheus registry каждого процесса;
- Go runtime/process metrics;
- gRPC/HTTP client/server metrics;
- PostgreSQL/Redis pool collectors;
- OpenTelemetry tracing;
- trace-aware `slog`;
- Sentry reporter;
- bounded cleanup.

Библиотека не регистрирует бизнесовые метрики и не принимает произвольные
metric labels. Service-specific metrics находятся в
`internal/observability/metrics`.

### `oidcidentity`

Общая канонизация стандартных OIDC identity claims:

- канонический UUID сохраняется без изменения семантики;
- opaque `sub`, `sid` и `jti` детерминированно преобразуются во внутренний UUID;
- namespace разделяется по точному issuer и виду идентичности;
- пустые, неограниченные и содержащие недопустимые символы значения
  отклоняются закрыто.

Модуль не проверяет подпись, audience, scope, roles и lifetime токена. Эти
проверки остаются в verifier конкретной trust boundary. Namespace UUID и
алгоритм канонизации считаются persisted contract и не меняются без миграции.

### `cache`

Общий read-through engine:

- provider-neutral key/value store;
- protobuf codec;
- bounded operation timeout;
- singleflight для cache miss одного процесса;
- hit/miss/error observer с закрытыми outcomes;
- fail-open к PostgreSQL при инфраструктурной ошибке Redis.

Сервис владеет:

- формированием и хэшированием cache key;
- protobuf snapshot schema;
- TTL и generation/invalidation policy;
- преобразованием snapshot через domain constructors;
- решением, допустим ли cache конкретной query.

Кэш не разрешает доступ при ошибке PostgreSQL и не становится источником
бизнесового состояния.

### `eventing`

Общий контур событий:

- canonical `Envelope`;
- validation и digest;
- provider-neutral `Publisher`;
- PostgreSQL outbox store;
- bounded relay с lease/retry/dead letter;
- PostgreSQL durable inbox/cursor;
- consumer processor;
- bounded technical metrics.

Подпакет/модуль `natsjetstream` реализует `Publisher` через актуальный NATS Go
JetStream API. Он владеет TLS/credentials connection, exact stream check,
publish acknowledgement и error classification, но не знает доменный payload.
`EnsureStream` разрешён только release-managed bootstrap job: он создаёт
отсутствующий exact stream, но не обновляет несовместимый существующий ресурс.

Сервис владеет AsyncAPI, `eventName`, payload, sequence, ordering key,
миграциями runtime-таблиц, точной NATS-конфигурацией и consumer effect.

### `internalrpcauth`

Общая межсервисная application security boundary:

- strict ES256 JWS/JWKS verification;
- exact algorithm/key binding;
- canonical claims;
- workload/SPIFFE binding;
- exact RPC permission policy;
- bounded token lifetime;
- устойчивое replay reservation;
- staged key rotation.

Сервис передает собственную permission matrix и разрешает aggregate ownership
в домене. Библиотека не определяет бизнесовые роли.

### `sqlquery`

Строгая загрузка именованных SQL:

- один embedded `.sql` на production query;
- точный `-- name: <query> :one|:many|:exec`;
- совпадение имени файла, имени query и cardinality;
- отказ при пустом, неизвестном или дублированном запросе;
- отсутствие runtime directory scanning.

Фактический SQL и `pgx.StrictNamedArgs` остаются в PostgreSQL adapter сервиса.

### `securetoken`

Низкоуровневые безопасные primitives:

- cryptographically secure random tokens;
- HMAC/hash с key ID;
- constant-time comparison;
- staged rotation current/previous/write key;
- zero/plaintext-minimizing API.

Библиотека не выбирает TTL, назначение token или бизнесовую политику отзыва.

### `securefile`

Единая проверка projected credential/config files:

- symlink остаётся внутри точного mount root;
- читается только regular file ограниченного размера;
- допустимы только exact read-only modes `0400`, `0440` и `0444`;
- write/execute permissions, пустой или изменившийся при чтении файл приводят к
  закрытому отказу;
- ошибки не раскрывают путь и содержимое.

Библиотека не определяет формат credential и не делает `0444` безопасным вне
изолированного read-only container mount. Эта граница доказывается итоговым
Kubernetes render по `GUIDE-DOC-003`.

### `postgrescredential`

Управляемый lifecycle PostgreSQL credentials:

- разделение runtime/migrator/reconciler roles;
- staged rotation;
- проверка нового подключения до отзыва старого;
- safe diagnostics без DSN/password;
- provider-neutral reconciliation interfaces.

Managed PostgreSQL может отключить in-cluster reconcilers и передать готовый
DSN/Vault secret. Runtime service от этого не меняется.

## Допустимые зависимости

```text
standard library
        ^
provider-neutral leaf modules
        ^
provider adapters (redisstore, natsjetstream, postgres*)
        ^
service composition root
```

- Domain service не импортирует `libs/go`, если примитив не является частью
  устойчивого доменного порта.
- Provider-neutral package не импортирует provider SDK.
- Provider adapter может импортировать provider-neutral API и один SDK.
- Общая библиотека не импортирует `services/**`.
- `observability` не становится обязательной зависимостью доменного API:
  библиотеки принимают узкий observer либо no-op.

## Context, ошибки и логи

- Public operation принимает `context.Context` первым аргументом.
- Библиотека не хранит request context в struct.
- Библиотека не создает root context и не скрывает goroutine.
- Ошибка классифицируется и возвращается наверх; логируется ровно на
  утвержденной process/transport/job boundary.
- Error text и runtime diagnostics пишутся на английском.
- Secret, payload, SQL, DSN, token, PII и arbitrary provider error не
  возвращаются наружу.

Если библиотеке нужны метрики, она принимает observer с закрытым набором
outcomes. Имя event, ID, URL, SQL operation, error text и пользовательское
значение не становятся label.

## Конфигурация

Общая библиотека получает типизированный config от composition root и
валидирует собственные инварианты. Она не читает env самостоятельно.

Service config:

1. читает env один раз;
2. валидирует обязательность и cross-field constraints;
3. формирует config общей библиотеки;
4. для production вызывает отдельную security validation, если локальный
   профиль допускает ослабленные настройки.

Пути к Vault-mounted credentials и TLS files передаются как абсолютные
очищенные пути. Значения секретов не копируются в публичную конфигурацию.

## Compatibility

- Удаление или изменение public API требует миграционного плана потребителей.
- Storage/wire schema общей библиотеки versioned и проверяется readiness.
- Общая библиотека не применяет миграции скрыто.
- Каждый сервис включает нужные forward-only migrations в собственный
  migration binary.
- Provider adapter можно заменить без изменения provider-neutral API и
  доменного кода.

## Запрещенные варианты

- `libs/go/common`, `utils`, `helpers` без узкой ответственности;
- общий module со всеми библиотеками и транзитивными SDK;
- бизнесовый enum или repository port конкретного сервиса в `libs/go`;
- глобальный logger/metrics registry/client singleton;
- чтение env или Vault внутри domain/shared operation;
- скрытый retry неидемпотентной команды;
- логирование одной ошибки на каждом слое;
- локальная копия outbox, inbox, gRPC interceptors или NATS publisher внутри
  сервиса;
- общий SQL repository на несколько сервисных БД;
- общий event payload, который заменяет AsyncAPI конкретного домена.
