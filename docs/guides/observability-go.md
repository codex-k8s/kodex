---
id: GO-DOC-003
title: Наблюдаемость Go-сервисов
type: guide
status: approved
owner: developer
version: 1.1.4
updated: 2026-09-07
---

# Наблюдаемость Go-сервисов

`GO-DOC-003` задает единый контракт логов, метрик, traces и Sentry для
внутренних сервисов, gateway и фоновых задач. Доменная эксплуатационная модель
остается в `OPS-DOC-001`, а этот документ определяет способ реализации.

## Граница ошибок и логи

Общая readiness владельца состояния не зависит от downstream worker, которому
этот же владелец нужен для startup/authority. Иначе удаление Service endpoint
создаёт цикл отказа. Health фонового пути и допустимость его результата остаются
отдельными: transport failure не создаёт observation, пустой claim не доказывает
готовность capability. Собственные инфраструктурные проверки остаются закрытыми.

Проверка readiness, которая создаёт временные файлы в рабочем томе,
согласует собственные scan/write/cleanup с другими служебными проверками,
публикацией результата и очисткой того же тома. Если операции выполняются
разными процессами, одного mutex недостаточно. Ожидание согласования
ограничивается caller context и отдельным бюджетом; crash освобождает
блокировку. Нельзя устранять гонку исключением пользовательских путей из
security/quota обхода или безусловным игнорированием filesystem ошибок.

- Ошибка логируется ровно один раз на transport boundary: HTTP error
  middleware, gRPC error/recovery interceptor, entry point job или fan-in
  параллельной операции.
- Domain service, repository и client возвращают ошибку с осмысленным `%w`,
  но не логируют ее.
- `InvalidArgument`, `Unauthenticated`, `PermissionDenied`, `NotFound`,
  `AlreadyExists`, `FailedPrecondition` и `Canceled` не являются server error.
- `Internal`, `Unavailable`, `Unknown`, `DataLoss` и panic записываются с
  `method`, `code`, проверенным `correlation_id`, а при наличии span — с
  `trace_id` и `span_id`.
- Классификация ожидаемых и неожиданных кодов задается одним переиспользуемым
  предикатом. Interceptor, logger и Sentry observer не поддерживают
  независимые неполные списки кодов.
- Controlled degradation логируется только в месте принятия fallback-решения и
  не повторяется на верхней границе.
- Runtime log messages, attribute keys и тексты ошибок — на английском.

Логи и error reporting не содержат request/response body, gRPC metadata, SQL,
Redis command arguments, DSN, token, cookie, телефон, email, внешний subject и
иные персональные данные.

CLI boundary не сериализует цепочку ошибок через `%v`/`Error()`, даже если
наружный domain error обычно имеет безопасный текст: иной startup path может
вернуть driver error с секретами. Для диагностики отказа достаточно закрытой
typed stage, нормализованного domain kind и error class; SQLSTATE выводится
только по явному allowlist. Joined timeout/cancel фиксируется отдельно от
первичного отказа, чтобы окончание retry budget не скрывало PostgreSQL или
domain rejection. Sentinel tests включают вложенные ошибки и неизвестные поля.

## Общая библиотека

Composition root читает shared telemetry env через `RuntimeConfigFromEnv`.
Ручная неполная копия `RuntimeConfig` не должна терять `Disabled`, Sentry pins
или sampler; сервисный Config не дублирует required shared env, иначе disabled
профиль ошибочно требует отсутствующие exporters. Prometheus при отключении
exporters остаётся рабочим; enabled-профиль сохраняет все проверки TLS/DSN.

Переиспользуемый код находится в `libs/go/observability`:

- отдельный Prometheus registry с меткой `service`;
- Go/process/build collectors;
- gRPC и HTTP middleware;
- collectors состояния pgxpool и go-redis pool;
- OTLP/gRPC tracer provider и W3C `tracecontext`/`baggage`;
- готовая инструментация `otelgrpc`, `otelpgx` и `redisotel`;
- `slog` handler с trace identifiers;
- Sentry observer для `libs/go/grpcserver`.

Сервисный пакет `internal/observability/metrics` содержит только бизнесовые
метрики и закрытые локальные технические события, которые нельзя обобщить.

## Метрики

Обязательные технические семейства:

- Go runtime: heap, GC, goroutines, scheduler;
- process: CPU, resident memory, file descriptors, start time;
- gRPC/HTTP: request rate, code, duration histogram, in-flight;
- PostgreSQL: acquired/idle/total/max/constructing, acquire count/duration,
  waits/cancel, created/destroyed connections;
- Redis: total/idle/pending, hit/miss/wait/timeout, stale/unusable connections.

Разрешены только labels из закрытых множеств: `service`, зарегистрированный
`method`/`route`, canonical `code`, `pool`, `role`, `capability`, `operation`,
`outcome`. Идентификаторы пользователей, организаций, запросов, correlation,
trace, SQL и URL labels запрещены.

HTTP method нормализуется закрытым allowlist стандартных методов. Любое
расширение или произвольный method token отображается в одно значение `OTHER`;
сырой `request.Method` не передается в metric vector. Аналогично неизвестные
route, operation, provider status и outcome отображаются в заранее объявленное
fallback-значение, а не создают новую серию.

Streaming server metrics и trace охватывают весь RPC от входа в общую цепочку,
включая auth, malformed request, admission и отказ до первого сообщения.
In-flight gauge увеличивается до этих проверок и гарантированно уменьшается при
любом исходе. Correlation назначается сервером один раз на stream, доступен
error boundary и success path, но никогда не используется как metric label.

Бизнесовые метрики описывают устойчивые outcomes утвержденных доменных
сценариев и transactional outbox, а не произвольные идентификаторы.

Если use case возвращает частичный результат вместе с ошибкой, уже совершенные
внешние действия и устойчивые изменения состояния учитываются в метриках до
регистрации error outcome цикла. Например, принятые провайдером сообщения и
выполненные expiration не исчезают из счетчиков из-за последующей ошибки
получения следующего элемента. Частичный результат не превращает цикл в
успешный: метрики действий и outcome цикла изменяются независимо.

Этот инвариант действует на всей цепочке возврата: adapter не заменяет
измеренный частичный результат пустым при последующей ошибке credential
refresh, захвата архива, cleanup либо доставки callback. Ошибочный transport
envelope переносит только ранее проверенные измерения и безопасные факты;
он не подтверждает итог исполнения или внешний effect. Terminal delivery
использует самостоятельный ограниченный бюджет и неизменный payload при
повторе. Grace периода Pod должно хватать на последовательные обязательные
остановку provider, credential callback, terminal delivery и cleanup.

## Traces

- Экспорт выполняется по OTLP/gRPC; Jaeger является backend, а не импортом в
  бизнес-код.
- `service.name`, `service.version` и `deployment.environment.name`
  обязательны.
- Входящий W3C context продолжается; sampling — `ParentBased` с
  настраиваемым ratio.
- gRPC spans создаются стандартным `otelgrpc` stats handler.
- pgx использует `otelpgx` без SQL statement, query parameters и connection
  details.
- Redis использует `redisotel` с выключенным `db.statement`.
- Shutdown обязан дождаться batch exporter в собственном ограниченном бюджете.
- Недоступный collector не меняет бизнесовый ответ, но ошибка экспорта видна в
  диагностике SDK.

## Sentry

Sentry используется только для неожиданных ошибок и panic:

- DSN хранится в Kubernetes Secret с encryption at rest и exact RBAC;
- client создается без глобального Hub и без default PII;
- event содержит synthetic exception, method/code/correlation/trace tags;
- request, metadata, исходная ошибка и значение panic не отправляются;
- expected domain/client errors не создают event;
- shutdown вызывает bounded flush с собственным контекстом, независимым от
  tracing shutdown и других cleanup-операций.

Sentry performance tracing не включается: распределенные traces принадлежат
OpenTelemetry.

## Сетевой путь telemetry

Включенная интеграция имеет реальный ограниченный сетевой путь. Наличие DSN,
OTLP endpoint или созданного SDK client при блокирующей `NetworkPolicy` не
считается работающей наблюдаемостью.

- Внутренний OTLP exporter обращается к exact namespace/service/port.
- Внешний Sentry либо другой SaaS использует allowlisted egress proxy, если
  устойчивый exact destination нельзя выразить `NetworkPolicy`.
- Application policy разрешает только app→proxy; proxy разрешает только
  утвержденные hostname/port и запрещает private, loopback, link-local и
  cluster destinations.
- `HTTPS_PROXY` и `NO_PROXY` не направляют внутренние service calls наружу.
- Открытый outbound HTTPS/443 для всего pod не является допустимой заменой.

Недоступность необязательного exporter не меняет бизнесовый ответ, но не
скрывается: состояние exporter/proxy отражается bounded metric, alert и
диагностикой без DSN, URL path, payload или credential. Если telemetry
dependency объявлена обязательной конкретным эксплуатационным профилем, она
входит в startup/readiness с bounded timeout.

## Composition root

Порядок подключения:

1. проверить типизированную конфигурацию;
2. создать registry, Sentry и tracer provider;
3. установить tracing в pgx config до создания pool;
4. инструментировать Redis client до первого запроса;
5. зарегистрировать pool collectors и бизнесовые metrics;
6. подключить gRPC stats handler и цепочку
   `technical metrics -> correlation -> business metrics -> recovery -> error`;
7. обернуть технический HTTP mux общим middleware;
8. при shutdown остановить servers, отменить и дождаться workers, затем
   выполнить flush traces и Sentry с независимыми bounded contexts.

Composition root принимает два явно различимых контекста по `GO-DOC-001`:

- `lifecycleCtx` отменяется сигналом процесса и управляет startup, серверами и
  workers;
- `backgroundCtx` создается один раз в `main` и используется только как
  родитель для bounded shutdown/flush contexts.

Tracing shutdown и Sentry flush не используют уже отмененный `lifecycleCtx`.
Каждая операция получает отдельный context и собственный deadline от
`backgroundCtx`; один context нельзя последовательно переиспользовать между
ними. `libs/go/observability` и сервисные пакеты не создают
`context.Background()` и не скрывают управление жизненным циклом: контекст
передает вызывающий composition root.

## Dashboards и алерты

Четыре общих dashboard устанавливаются один раз из
`deploy/k8s/base/observability` и имеют переменную `service`:

1. Go runtime/process;
2. HTTP/gRPC;
3. PostgreSQL;
4. Redis.

Сервис не копирует эти ConfigMap в собственный overlay. Его отдельный dashboard
принадлежит бизнесовому домену и располагается рядом с манифестами сервиса.
ConfigMap dashboard имеет label `grafana_dashboard=1`; Grafana sidecar читает
такие ConfigMap во всех namespace.

Матрица размещения:

| Артефакт                                                           | Каноническое место                                         | Владение                 |
| ------------------------------------------------------------------ | ---------------------------------------------------------- | ------------------------ |
| Registry, runtime/transport/pool metrics, tracing и Sentry runtime | `libs/go/observability`                                    | общий Go-код             |
| Общие gRPC error/recovery interceptors                             | `libs/go/grpcserver`                                       | общий Go-код             |
| Бизнесовые метрики и bounded outcomes                              | `services/<zone>/<service>/internal/observability/metrics` | один сервис              |
| Четыре технических dashboard с селектором `service`                | `deploy/k8s/base/observability/dashboards`                 | один экземпляр на контур |
| Бизнесовый dashboard                                               | `deploy/k8s/base/<service>/dashboards`                     | один сервис              |
| `ServiceMonitor` и правила alerts с service thresholds/runbook     | `deploy/k8s/base/<service>`                                | один сервис              |

Общий dashboard не импортируется как ресурс service kustomization. Общий
observability overlay применяется bootstrap-процессом один раз, а service
overlay независимо поставляет сервисные monitor, alerts и бизнесовый
dashboard. Dashboard UID глобально уникален: копия общего UID в другом
namespace также считается ошибкой.

Минимальные alerts покрывают server error rate, p99 latency, насыщение и
отмены PostgreSQL pool, Redis timeouts и критичные бизнесовые outcomes. Каждый
alert содержит severity и абсолютный HTTPS URL доступного runbook. Относительный
путь репозитория не является рабочей ссылкой для Alertmanager или Grafana.
`runbook_url` каждого правила обязан быть абсолютным HTTPS URL.

Принадлежащий deployable dashboard не ограничивается транспортом. Для
применимых зависимостей и сценариев он показывает:

- частоту запросов, ошибки, latency/in-flight и startup/readiness;
- Go heap, goroutines, GC/scheduler, CPU и process limits;
- каждый именованный runtime/relay PostgreSQL pool и Redis pool;
- outbox/inbox claimed, published/processed, retry, terminal/dead-letter,
  ordering blockage и repair outcome;
- защищённый жизненный цикл: queued/claimed/running/waiting-owner/continuation,
  expiry lease, grants/claims и конечные исходы;
- критичные доменные изменения и счётчики частичных эффектов независимо от
  итогового исхода цикла.

Каждый запрос панели использует только закрытые labels из этого документа.
Произвольный resource ID, event name, route, status, ответ provider или текст
ошибки не добавляется ради детализации. Alert для terminal/blocked path
ссылается на точный абсолютный HTTPS runbook, который содержит авторитетные
read/repair и запрет ручного изменения состояния.

## Проверки

Профиль определяется `GOV-DOC-003`. До staging проверяются закрытая
кардинальность labels, отсутствие PII, полный unexpected-code predicate,
независимый shutdown exporters, render dashboards/alerts и доступность
абсолютных HTTPS `runbook_url`.

## Проверенная документация библиотек

На 2026-07-25 через Context7 проверены:

- Sentry Go SDK: отдельный client/Hub, capture и bounded flush;
- OpenTelemetry Go/contrib: OTLP exporter, resource, propagation,
  `otelgrpc.NewServerHandler`;
- Prometheus `client_golang`: custom registry, Go/process/build collectors,
  `promhttp.HandlerFor` и `InstrumentMetricHandler`.

Используемые версии закрепляются в `libs/go/observability/go.mod`.

Связанные документы: `GO-DOC-001`, `GUIDE-DOC-003`, `INFRA-DOC-001`.
