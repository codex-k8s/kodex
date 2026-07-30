---
id: GO-DOC-001
title: Серверная разработка на Go
type: guide
status: approved
owner: developer
version: 1.2.0
updated: 2026-07-30
---

# Серверная разработка на Go

`GO-DOC-001` задает обязательные границы и сквозной способ реализации
Go-сервиса. Каноническое дерево каталогов приведено в `REPO-DOC-001`,
PostgreSQL и миграции - в `GO-DOC-002`, надежная доставка событий - в
`GO-DOC-004`, межсервисное взаимодействие - в `GO-DOC-005`, а общие
библиотеки - в `GO-DOC-006`.

## Направление зависимостей

```text
cmd -> internal/app
internal/app -> transport + clients + repository adapters + domain services
transport -> domain services + domain types
clients -> generated contracts + domain-safe client models
repository adapters -> domain repository ports + domain types
cache decorator -> domain repository port + shared cache API + domain types
domain service -> domain repository ports + domain types + domain errors
domain types -> стандартная библиотека и минимальные доменные зависимости
```

Обратные зависимости запрещены. Домен не импортирует `app`, `transport`,
`clients`, PostgreSQL driver, generated contract DTO или конфигурацию процесса.
Transport не обращается к PostgreSQL adapter напрямую.

## Сквозной путь операции

Каждая RPC query проходит один и тот же путь:

```text
generated gRPC request
-> общие server interceptors
-> transport server method
-> request caster
-> domain query/input
-> domain service operation
-> repository/client port
-> PostgreSQL/cache/downstream adapter
-> domain result
-> response caster
-> generated gRPC response
```

Каждая state-changing command проходит расширенный путь:

```text
generated gRPC request
-> authentication/authorization interceptors
-> request caster
-> domain command
-> domain service
-> repository.Transact
-> idempotency receipt + authority resolution
-> aggregate load/lock + OCC check
-> aggregate method
-> persistence + audit + outbox append
-> one PostgreSQL commit
-> domain result
-> response caster
-> generated gRPC response
```

Ни один слой нельзя пропустить скрытым shortcut:

- transport не вызывает SQL, cache или broker;
- domain service не принимает generated request и не возвращает generated
  response;
- repository adapter не принимает решение о допустимости бизнесового
  перехода;
- relay не строит доменное событие и не исправляет его payload;
- client adapter не возвращает generated response в domain service.

Для каждой операции разработчик должен уметь показать этот путь по конкретным
файлам. Если часть пути не нужна, отсутствие фиксируется явно: например,
read-only query не создает outbox event, а локальная command без внешних
потребителей не получает фиктивное событие.

## Ответственность слоев

| Слой | Обязанности | Запрещено |
| --- | --- | --- |
| `cmd/<binary>` | root context, signals, запуск `app` | бизнес-логика, SQL, ручная сборка зависимостей |
| `internal/app` | config, composition, startup barrier, readiness, shutdown | сценарии использования, transport branching |
| `internal/transport/<protocol>` | protocol validation, casters, вызов сервиса, error mapping | SQL, domain policy, прямой broker publish |
| `internal/domain/types` | entities, values, enums, queries и их инварианты | transport/driver DTO, config процесса |
| `internal/domain/service` | command/query orchestration и бизнесовые переходы | gRPC/HTTP codes, SQLSTATE, broker SDK |
| `internal/domain/repository` | persistence ports и атомарные transaction ports | pgx types, SQL, реализация adapter |
| `internal/clients` | адаптация generated downstream client к domain-safe port | бизнесовое владение чужими данными |
| `internal/integration` | внешний provider SDK/API за узким портом | распространение provider DTO по домену |
| `internal/repository` | PostgreSQL/cache/object-storage adapters | новые бизнесовые правила |
| `internal/maintenance` | bounded cleanup/reconciliation внутри владельца данных | отдельный скрытый workflow без lifecycle |
| `internal/observability` | только бизнесовые метрики сервиса | копии общего runtime и произвольные labels |
| `internal/generated` | воспроизводимый результат codegen | ручное редактирование |

`internal/authorization` допустим только как service-specific adapter общей
межсервисной security boundary. Он не становится вторым доменом авторизации и
не дублирует общие verifier/replay primitives из `libs/go`.

## Read-through кэш

Кэш repository query оформляется декоратором того же доменного порта:

- PostgreSQL остается авторитетным источником;
- Redis хранит версионированный protobuf-снимок с ограниченным TTL;
- ключ с пользовательскими идентификаторами хэшируется;
- значение после чтения снова проходит доменные constructors;
- одновременные cache miss одного процесса объединяются;
- ошибка Redis ведет к чтению PostgreSQL, а не к отказу доступности;
- ошибка PostgreSQL не маскируется stale или синтетическим разрешением.
- SQL ограничивает объем выборки значением `limit + 1`, а превышение
  согласованной границы закрывается отказом без частичного результата.

Общий engine находится в `libs/go/cache` и не зависит от Prometheus.
Сервис передает observer с закрытыми низкокардинальными событиями. TTL
решений доступа по умолчанию равен 10 секундам и не может быть больше минуты
без отдельного решения о допустимом окне отзыва доступа.

## `cmd` и `internal/app`

`cmd/<service>/main.go` выполняет только запуск процесса и передачу управления
в `internal/app`. В `main.go` запрещены:

- бизнес-логика;
- SQL;
- регистрация каждого handler вручную;
- чтение отдельных env без единой валидации;
- создание глобальных singleton-зависимостей.

`internal/app` является composition root:

- читает и валидирует config;
- создает подключения и adapters;
- передает repository ports в domain services;
- создает transport servers;
- управляет readiness, graceful shutdown и закрытием ресурсов.

Общие config, lifecycle, readiness и технический HTTP endpoint подключаются из
`libs/go/serviceruntime` и `libs/go/httpserver`. Production mTLS и единая gRPC
error/recovery boundary подключаются из `libs/go/grpcserver`. Локальные копии
этих примитивов в `internal/app` запрещены: composition root только задает
service-specific зависимости, политику готовности и порядок shutdown.

`internal/app` не становится вторым доменным слоем и не содержит сценариев
использования.

## Контексты процесса и запросов

Production-бинарь имеет один источник корневого фонового контекста:

1. `main` создает `backgroundCtx := context.Background()`;
2. `run` выводит из него отменяемый сигналами `lifecycleCtx`;
3. `lifecycleCtx` передается в startup, listeners, workers и другие операции,
   которые должны остановиться при завершении процесса;
4. исходный `backgroundCtx` передается в composition root только для
   ограниченного по времени shutdown после отмены `lifecycleCtx`;
5. каждый shutdown/flush получает собственный
   `context.WithTimeout(backgroundCtx, timeout)`.

Минимальная форма:

```go
func main() {
    backgroundCtx := context.Background()
    if err := run(backgroundCtx); err != nil {
        os.Exit(1)
    }
}

func run(backgroundCtx context.Context) error {
    lifecycleCtx, stop := signal.NotifyContext(
        backgroundCtx,
        syscall.SIGINT,
        syscall.SIGTERM,
    )
    defer stop()

    return app.Run(lifecycleCtx, backgroundCtx)
}
```

В production-коде ниже `cmd/<binary>/main.go` запрещены
`context.Background()` и `context.TODO()`. Пакет, adapter или библиотека
принимает контекст вызывающего кода; request-scoped операция использует
контекст входящего запроса. Контекст не передается как `nil`, не сохраняется в
доменной сущности или глобальном singleton и не заменяется новым корневым
контекстом для обхода отмены.

Отмененный `lifecycleCtx` нельзя использовать для обязательного shutdown:
flush telemetry и закрытие удаленного ресурса завершатся немедленно. Нельзя и
использовать неограниченный `backgroundCtx` напрямую: cleanup всегда получает
deadline. Если операция должна пережить клиентский запрос, это отдельная
явная задача процесса с собственным lifecycle, а не скрытый
`context.Background()` внутри handler или goroutine.

Composition root запускает background workers только после успешного создания
всех обязательных listeners, построения transport servers и завершения
остального startup. До этой startup barrier worker не выполняет polling,
dispatch, maintenance и иные внешние или необратимые действия.

Версия goose не заменяет проверки подключенных runtime-компонентов. Сервис,
который записывает transactional outbox, регистрирует публичный
`postgresoutbox.Store.Check` как обязательную readiness dependency даже до
подключения relay. Сервис с consumer дополнительно регистрирует
`postgresinbox.Processor.Check`. Ошибка любой такой проверки завершает startup
до открытия listeners и запуска workers.

Composition root выводит единый внутренний worker context из `lifecycleCtx` и
владеет его cancel/join-контрактом. При любом выходе он:

1. переводит readiness в состояние отказа и прекращает принимать новую работу;
2. отменяет worker context;
3. ограниченно останавливает transport servers;
4. ограниченно дожидается завершения всех workers;
5. только после этого закрывает PostgreSQL, Redis и другие зависимости workers.

Запуск worker через отдельный `go` без регистрации в общей группе ожидания
запрещен. Ошибка startup, одного transport server или worker не должна
оставлять фоновую goroutine, способную продолжить внешнее действие во время
cleanup.

Каждая обязательная cleanup-операция получает собственный
`context.WithTimeout(backgroundCtx, timeout)`. Последовательная передача одного
контекста в tracing shutdown, Sentry flush и другие независимые операции
запрещена: первая зависшая операция не должна исчерпать бюджет остальных.

## Доменные типы

### `types/entity`

Здесь живут сущности и агрегаты, которые имеют идентичность и жизненный цикл.
Сущность:

- создается валидирующим конструктором;
- не допускает недействительного публичного состояния;
- изменяется методами, которые проверяют инварианты;
- не содержит JSON/SQL/gRPC-specific поведения, если это не часть устойчивого
  доменного контракта.

Файл называется по связной группе сущностей. Один огромный `entities.go`
разделяется, когда типы перестают образовывать одну связную модель.

### `types/value`

Value object определяется значением, а не идентификатором. В нем находятся
идентификаторы, деньги, интервалы, координаты, нормализованные коды и другие
проверяемые значения.

- Нельзя создавать value object в обход конструктора, если существуют
  инварианты.
- Slice, map и pointer state копируются на входе и выходе, если иначе внешний
  код может изменить доменное состояние.
- Строковое представление не заменяет тип там, где значения имеют разные
  доменные смыслы.

### `types/enum`

Здесь находятся именованные типы и константы закрытых наборов: статусы, роли,
виды операций и режимы. Для enum задаются:

- явный underlying type;
- полный список допустимых значений;
- валидация неизвестного значения на входной границе;
- отсутствие скрытого значения по умолчанию, если нулевое значение невалидно.

### `types/query`

Здесь находятся доменные фильтры, page requests и read-модели, которые нужны
порту repository или domain service. HTTP query parameters, protobuf requests
и SQL rows сюда не переносятся.

## Доменные сервисы

Сценарии использования находятся в
`internal/domain/service/<capability>`. Пакет содержит:

- `service.go` - зависимости, конструктор и общие инварианты capability;
- `<operation>.go` - связный command/query сценарий;
- `types.go` - входы и результаты сценариев;
- вспомогательные типы рядом со сценарием, которому они принадлежат.

Domain service:

- принимает доменные inputs, а не transport DTO;
- зависит от узких repository/client ports;
- задает последовательность доменных действий и транзакционные намерения;
- возвращает безопасные доменные ошибки;
- не знает про HTTP status, gRPC code, SQLSTATE и UI.

Пакет `application` рядом с `domain` не создается. Его обязанности уже разделены
между `internal/domain/service` и `internal/app`.

### Query

Query не изменяет авторитетное состояние. Она:

1. валидирует domain input;
2. разрешает owner/tenant boundary из доверенного контекста;
3. вызывает read port или типизированный downstream client;
4. возвращает доменную read model;
5. не маскирует ошибку источника истины кэшированным разрешением.

Даже если query использует один repository method, она остается методом
domain service: transport не получает прямой доступ к repository. Исключение
допустимо только для общего технического health endpoint, который не является
бизнесовым API.

### Command

Command задает одну бизнесовую транзакционную границу. Типовой алгоритм:

1. проверить синтаксические и доменные инварианты входа;
2. вычислить устойчивый hash idempotency key и request;
3. открыть transaction через доменный repository port;
4. проверить существующий command receipt;
5. разрешить текущий aggregate внутри доверенной owner/tenant boundary;
6. проверить expected version;
7. вызвать метод aggregate;
8. сохранить aggregate и связанные изменения;
9. записать audit и каждое обязательное domain event;
10. сохранить результат command receipt;
11. commit один раз;
12. вернуть доменный результат.

Совпавший idempotency key с тем же request hash возвращает ранее сохраненный
результат. Тот же key с другим request hash закрывается конфликтом. Проверка
idempotency не заменяет authority и OCC.

Конкурентная команда с тем же key не создает два effect. Unique conflict
вставки command receipt классифицируется PostgreSQL adapter как retryable
transaction conflict; ограниченный повтор той же domain operation перечитывает
уже зафиксированный receipt. Serialization/deadlock retry использует тот же
механизм и тот же request context.

Если операция не может атомарно сохранить бизнесовое состояние, receipt,
audit и outbox, граница выбрана неверно. Публикация события или внешний вызов
до commit запрещены. Необратимый внешний effect моделируется отдельной task
или событием после устойчивой фиксации намерения.

## Repository ports

Интерфейс хранилища принадлежит домену и находится в
`internal/domain/repository/<capability>`.

- Методы выражают доменное намерение, а не CRUD таблиц.
- В сигнатурах используются domain types.
- Интерфейс не раскрывает `pgx.Rows`, транзакцию, SQL, DTO базы данных или
  driver errors.
- Один интерфейс отвечает за связный aggregate/capability; общий repository на
  весь сервис допустим только при доказанной общей атомарной границе.
- Domain service зависит только от этого порта и не знает о конкретном adapter.

PostgreSQL adapter реализует порт в `internal/repository/postgres/<capability>`
и содержит compile-time assertion реализации интерфейса.

Для capability с изменениями рекомендуемый порт разделяется на чтение и
транзакцию:

```go
type Reader interface {
    Get(context.Context, AggregateID) (Aggregate, error)
    List(context.Context, Filter) (Page[Aggregate], error)
}

type Repository interface {
    Reader
    Transact(context.Context, func(Transaction) error) error
}

type Transaction interface {
    Reader
    GetForUpdate(context.Context, AggregateID) (Aggregate, error)
    Save(context.Context, Aggregate, ExpectedVersion) error
    GetCommandResult(context.Context, IdempotencyKeyHash) (CommandResult, error)
    SaveCommandResult(context.Context, CommandResult) error
    AppendEvent(context.Context, DomainEvent) error
}
```

Это форма доменного порта, а не утечка `pgx.Tx`: callback получает интерфейс
`Transaction`, реализация которого принадлежит PostgreSQL adapter. Если
несколько aggregates действительно изменяются атомарно, transaction port
владеет capability, которая выражает это доменное намерение; общий
`UnitOfWork` на весь монорепозиторий запрещен.

Read-through cache реализует только read subset того же порта и декорирует
PostgreSQL adapter в `internal/app`. Write path всегда идет в источник истины.
Invalidation/generation является частью сервисного adapter, а не общей
cache-библиотеки.

## Ошибки

Доменные sentinel/typed errors находятся в `internal/domain/errs`. Ошибка:

- сообщает вызывающему коду тип результата;
- имеет безопасный английский текст для runtime-диагностики;
- не содержит secret, PII, SQL text и сырую ошибку внешнего SDK;
- сохраняет техническую причину для безопасного внутреннего логирования через
  wrapping на adapter boundary;
- отображается в HTTP/gRPC code только в transport.

Не сравнивать текст ошибок. Использовать `errors.Is`/`errors.As` и стабильные
типы.

Все runtime-логи, CLI/API error messages и Prometheus `HELP` пишутся на
английском. Комментарии к коду и документация остаются на русском.
Повторяющийся диагностический текст и ключ структурированного лога выносятся в
именованную константу в минимальной общей области.

## Transport

### gRPC

Внутренний сервис использует контракт Proto/gRPC из `contracts/`.

- Generated server/client code не редактируется.
- `casters/requests.go` преобразует generated request в domain input и
  отклоняет недействительные данные.
- `casters/responses.go` преобразует domain result в generated response.
- `server.go` вызывает domain service и не содержит бизнес-ветвлений;
- один server method остается коротким: cast request, invoke service, cast
  response;
- transport-level metadata извлекается до caster и передается отдельным
  проверенным типом, а не добавляется в generated request;
- `errors.go` является единственным service-specific отображением domain error
  в gRPC status;
- переиспользуемые auth, timeout, recovery, error-mapping и correlation
  interceptors берутся из `libs/go/grpcserver`;
- локальный transport содержит только доменное отображение ошибок и
  специфичную для сервиса оркестрацию;
- размер сообщения, число одновременных потоков и жизненный цикл соединений
  ограничиваются server options;
- проверенный `x-correlation-id` возвращается клиенту и используется в логах,
  но не становится высококардинальной меткой.
- `x-correlation-id` является каноническим непустым UUID; отсутствующее или
  произвольное значение заменяется сервером новым UUID до входа в handler.

### HTTP и WebSocket

Внешний HTTP/WebSocket transport находится в gateway. Если внутреннему сервису
нужна техническая HTTP-точка, она ограничивается health/metrics/debug policy и
не становится обходным бизнес-API.

## Clients

Клиент другого сервиса размещается в `internal/clients/<service>`.

- Используется готовый generated client версионированного контракта.
- Adapter задает timeout, retry и безопасное отображение ошибок.
- Adapter явно подключает все обязательные transport/application credentials.
  Наличие mTLS не разрешает пропустить bearer token, authorization context или
  другой слой, который требует принимающий сервис.
- Credential применяется к health/readiness и рабочим RPC одинаково, требует
  transport security и не попадает в environment dump, логи или ошибки.
- Domain service получает узкий client port или доменно безопасный adapter, а
  не generated client напрямую.

Client adapter располагается рядом с вызывающим сервисом и владеет:

- сборкой metadata и signed authorization context;
- deadline конкретной операции;
- retry только безопасных или идемпотентных RPC;
- преобразованием generated response в domain-safe result;
- отображением gRPC status в устойчивые ошибки вызывающего домена;
- bounded observability без payload, token и PII.

Downstream client не открывает circuit breaker внутри domain entity и не
кэширует authoritative ответ без явной сервисной политики. Синхронный вызов,
который необходим для commit локальной транзакции, требует отдельного анализа:
удерживать PostgreSQL transaction во время сетевого RPC по умолчанию
запрещено.
- SDK внешнего провайдера изолируется в отдельном adapter; provider-specific
  типы не протекают в домен.

## Gateway

Gateway является внешней транспортной границей:

- проверяет форму, размер и происхождение запроса;
- выполняет authentication/authorization policy и rate limit;
- вызывает внутренние сервисы через generated clients;
- агрегирует ответы только в пределах утвержденного API-сценария;
- не хранит копию доменных агрегатов и не реализует доменные state machine.

Если правило должно одинаково работать для нескольких gateway или фонового
процесса, оно принадлежит внутреннему сервису.

## Авторитетная выдача и eligibility

Сервис-владелец состояния задает один доменный предикат доступности ресурса
для каждого класса actor. Его нельзя независимо воспроизводить в gateway,
repository SQL и consumer projection.

- Одиночное и пакетное чтение применяют одно правило.
- Поисковая или иная публичная проекция применяет то же правило по событию.
- Скрытый ресурс возвращается неотличимо от отсутствующего, если caller не
  вправе знать о его существовании.
- Неизвестный lifecycle или сочетание статусов закрыто отклоняется.
- Разные оси состояния, например публикация, модерация, качество и полнота, не
  объединяются в один gate без явного доменного решения.

Gateway проверяет внешний transport и маршрутизирует вызов, но не становится
владельцем lifecycle. Staff/workload read path отделяется от публичного и не
расширяет его выдачу.

## Доменные события

Producer сохраняет бизнесовое изменение и событие одной PostgreSQL-транзакцией
через `libs/go/eventing/postgresoutbox`. Relay публикует сохраненный envelope
через узкий `eventing.Publisher`; broker SDK остается в adapter.

Consumer обрабатывает событие через
`libs/go/eventing/postgresinbox.Processor`: локальный durable effect, inbox и
cursor фиксируются атомарно. Redis, in-memory map и broker offset не заменяют
durable inbox.

Первый consumer не запускает broker subscription, пока relay producer,
publisher и inbox schema не прошли обязательный startup/readiness gate.
Ordering, retry, dead letter, retention и PITR определены в `GO-DOC-004`.

## Контракты и генерация

- Внешний HTTP API описывается OpenAPI.
- Внутренний синхронный API описывается Proto/gRPC.
- События и WebSocket payloads описываются AsyncAPI.
- Исходники контрактов находятся в `contracts/`.
- Generated code создается воспроизводимой командой и не правится вручную.
- Изменение контракта сохраняет воспроизводимую генерацию потребителей в
  применимой области.
- Каждое утвержденное вычисляемое поле достигает всех нужных read/event
  consumers. Метод доменной модели без вызова и без выхода в авторитетную
  read model не считается реализованным требованием.
- Сквозная проверка связывает domain field, repository snapshot, transport
  response, event payload и consumer action. Совпадение имен без совпадения
  semantics и lifecycle недостаточно.

## OCI-сборка Go-модулей

Multi-stage Dockerfile учитывает полный local-module closure основного
`go.mod`.

1. До `go mod download` копируются `go.mod`/`go.sum` основного и каждого
   локального replaced module.
2. До `go build` копируются исходники всех этих modules.
3. Docker context и ignore rules явно разрешают ту же closure.
4. Runtime и migration targets собираются из одного проверенного source tree,
   но имеют минимально разные entrypoint и содержимое.

Успешная локальная сборка вне container не доказывает OCI target. Отсутствующий
manifest replaced module, исключенный ignore rules каталог или mutable builder
image считается воспроизводимым отказом production artifact.

## Конфигурация

- Конфигурация каждого Go-сервиса, gateway и job описывается типизированной
  структурой и читается через `github.com/caarlos0/env/v11`.
- Единственный вызов `env.ParseWithOptions` выполняется в
  `internal/app/config.go`. Конфигурации clients и adapters являются вложенными
  типизированными фрагментами с `envPrefix`, но не читают environment повторно.
- Обязательные значения отмечаются `required`; строки, для которых пустое
  значение недопустимо, дополнительно отмечаются `notEmpty`.
- Ошибка чтения, преобразования или валидации конфигурации останавливает процесс
  до запуска listeners, workers и любых преобразований состояния.
- Секреты передаются ссылкой или через runtime environment и не сериализуются в
  диагностический вывод.
- Обязательный новый параметр имеет безопасный rollout path.
- Runtime defaults фиксируются в коде или версионированной конфигурации, а не в
  незафиксированном shell.

Минимальная форма:

```go
type Config struct {
    GRPC      GRPCConfig      `envPrefix:"GRPC_"`
    Database DatabaseConfig  `envPrefix:"DATABASE_"`
}

func ParseConfig() (Config, error) {
    var cfg Config
    if err := env.ParseWithOptions(&cfg, env.Options{}); err != nil {
        return Config{}, fmt.Errorf("parse config: %w", err)
    }
    if err := cfg.Validate(); err != nil {
        return Config{}, fmt.Errorf("validate config: %w", err)
    }
    return cfg, nil
}
```

Импорт `env` в примере означает `github.com/caarlos0/env/v11`. Запрещены
разрозненные `os.Getenv`, silent fallback для обязательных параметров,
`env.Must`/`panic` в библиотечных пакетах и вывод структуры конфигурации целиком.

## Наблюдаемость

Общий технический контур подключается из `libs/go/observability` по
`GO-DOC-003`. Сервис не копирует collectors, OTLP setup или Sentry middleware.
Локальный `internal/observability/metrics` содержит только бизнесовые outcomes.

Error/recovery boundary из `libs/go/grpcserver` является единственным местом
error-логирования и Sentry reporting для unary gRPC. Ожидаемые доменные ошибки
не отправляются как server error; request, metadata и персональные данные не
передаются observer.

Технические labels обязаны быть bounded. `correlation_id`, `trace_id`,
идентификаторы домена, URL и SQL никогда не становятся labels.

## Миграция существующего плоского кода

Путь `services/<service>/domain` не является временно допустимой альтернативой.
Рефакторинг выполняется атомарно:

1. типы классифицируются как `entity`, `value`, `enum` или `query`;
2. сценарии переносятся в `internal/domain/service/<capability>`;
3. repository interfaces переносятся в доменный порт;
4. adapters, SQL и migrations переносятся в канонические каталоги;
5. imports обновляются;
6. старый путь удаляется в том же PR;
7. запрещено оставлять alias/package forwarding как второй источник правды.

Для одновременной миграции нескольких сервисов владелец сначала принимает
структуру полного diff, и только после этого запускаются независимые functional
и security review.

## Проверки

Обязательный профиль линтеров, сборок и тестов определяется `GOV-DOC-003`.
Локальный результат привязывается к точному SHA и не выдается за GitHub CI.
В активной фазе `Prototype` выполняются быстрые
компиляция/сборка/форматирование/diff. Небольшой unit-тест добавляется только
для действительно сложной чистой либо security-critical логики; простому
composition/config/transport glue тест ради покрытия не нужен.

Отсутствие полного test coverage, integration/E2E,
contract/deploy/render/lifecycle/oracle suites и общего baseline до
завершения прототипа не блокирует Go unit и не является review finding.
Полная поддерживаемая стратегия будет отдельной owner-approved волной после
готовности прототипа:
[Issue #216](https://github.com/codex-k8s/matter-codex/issues/216).

Связанные документы: `REPO-DOC-001`, `GO-DOC-002`, `GO-DOC-003`,
`GO-DOC-004`, `GO-DOC-005`, `GO-DOC-006`, `GUIDE-DOC-003`,
`ARCH-DOC-001`.
