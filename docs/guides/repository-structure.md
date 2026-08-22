---
id: REPO-DOC-001
title: Структура репозитория и сервисов
type: guide
status: approved
owner: architect
version: 1.3.0
updated: 2026-08-23
---

# Структура репозитория и сервисов

`REPO-DOC-001` задает обязательное размещение кода в монорепозитории. Новый
компонент не вводит собственную схему каталогов: он выбирает один из
зафиксированных ниже шаблонов.

## Верхний уровень

- `contracts/` - версионированные источники правды OpenAPI, Proto/gRPC,
  AsyncAPI и межсервисных политик.
- `services/internal/` - внутренние доменные Go-сервисы.
- `services/external/` - внешние API-шлюзы и ingress-адаптеры.
- `services/jobs/` - самостоятельно развертываемые фоновые задачи и workers.
- `services/staff/` - PWA и служебные поверхности владельца и операторов
  MatterCodex.
- `deploy/` - Kubernetes-манифесты, overlays и deploy inventory.
- `infra/` - инфраструктурный код, bootstrap scripts и IaC.
- `tools/` - утилиты разработки и генерации.
- `libs/go/` - общие Go-примитивы с отдельными module и узким API.
- `docs/` - проектная документация.

Общие Grafana dashboards с выбором `service` размещаются и устанавливаются
единожды из `deploy/k8s/base/observability`. В service overlay хранится только
доменный dashboard конкретного сервиса.

### Kubernetes base

```text
deploy/k8s/base/
├── observability/
│   ├── kustomization.yaml                  # общие ресурсы контура
│   └── dashboards/
│       ├── go-runtime-dashboard.yaml       # один dashboard с service selector
│       ├── transport-dashboard.yaml        # один dashboard с service selector
│       ├── postgresql-dashboard.yaml       # один dashboard с service selector
│       └── redis-dashboard.yaml            # один dashboard с service selector
├── example-service/
│   ├── kustomization.yaml                  # runtime deployable сервиса
│   ├── deployment.yaml
│   ├── service.yaml
│   ├── service-monitor.yaml                # discovery именно этого сервиса
│   ├── prometheus-rule.yaml                # alerts и runbook этого сервиса
│   └── dashboards/
│       └── example-business-dashboard.yaml
│                                            # только бизнесовый dashboard сервиса
├── example-service-data/                   # PostgreSQL/Redis с отдельным lifecycle
└── example-service-migration/              # migration Job с отдельным lifecycle
```

`deploy/k8s/base/<capability>` содержит ресурс, устанавливаемый один раз на
кластер или эксплуатационный контур. `deploy/k8s/base/<component>` содержит
ресурсы одного deployable. Общий ресурс не копируется в component overlay и не
публикуется под новым именем: bootstrap или environment overlay применяет его
отдельно. Service-specific `ServiceMonitor`, alerts, business dashboard,
Deployment, Service и policy остаются рядом с сервисом.

## Реестр компонентов проекта

Фактический набор компонентов определяется `ARCH-DOC-004`, а не этим гайдом.
После согласования архитектуры проект заполняет таблицы:

### Внутренние сервисы

| Логическая граница | Канонический путь             |
| ------------------ | ----------------------------- |
| `<domain>`         | `services/internal/<service>` |

### Внешние gateway и PWA

| Поверхность          | Канонический путь                |
| -------------------- | -------------------------------- |
| `<audience>-gateway` | `services/external/<gateway>`    |
| staff PWA            | `services/staff/<application>`   |

### Фоновые задачи

| Компонент  | Канонический путь        | Владелец результата |
| ---------- | ------------------------ | ------------------- |
| `<worker>` | `services/jobs/<worker>` | `<domain-service>`  |

Worker исполняет attempt, но не становится владельцем доменного результата.
Task, immutable input, grant, retry и terminal state принадлежат внутреннему
сервису.

## Имена компонентов

- Имя каталога является стабильным именем компонента и записывается в
  `kebab-case`.
- Главный бинарный файл Go-сервиса находится в
  `cmd/<service-name>/main.go`.
- Один самостоятельно развертываемый Go-сервис является отдельным Go module.
- Код одного сервиса не импортирует `internal` другого сервиса. Взаимодействие
  идет через сгенерированный клиент версионированного контракта.
- По умолчанию пакет выносится в общий каталог после второго реального
  потребителя. Исключение — заранее утвержденный владельцем инфраструктурный
  примитив для нескольких будущих сервисов: он сразу размещается в `libs/go`,
  получает отдельный module и узкий provider-neutral API.

## Внутренний Go-сервис

Пример для `example-service`; остальные внутренние сервисы повторяют ту же
форму.

```text
services/internal/example-service/
├── Dockerfile                              # воспроизводимый runtime-образ сервиса
├── go.mod                                  # граница Go module и зависимостей
├── go.sum                                  # зафиксированные суммы зависимостей
├── cmd/
│   ├── example-service/
│   │   └── main.go                         # минимальная точка входа сервера
│   └── cli/
│       ├── main.go                         # команды migrate/status и служебные операции
│       └── migrations/
│           ├── embed.go                    # embed.FS и настройка goose
│           └── 20260728120000_example_service_initial.sql
│                                            # forward-only SQL-миграция goose
└── internal/
    ├── app/
    │   ├── app.go                          # composition root и жизненный цикл процесса
    │   └── config.go                       # единый env/v11 parse и валидация до запуска
    ├── authorization/
    │   └── adapter.go                      # service-specific policy общей RPC auth boundary
    ├── clients/
    │   └── dependency-service/
    │       ├── client.go                   # адаптер сгенерированного downstream-клиента
    │       └── config.go                   # типизированный env/v11-фрагмент downstream
    ├── domain/
    │   ├── commandreceipt/
    │   │   └── receipt.go                  # устойчивый результат идемпотентной команды
    │   ├── errs/
    │   │   └── errors.go                   # безопасные доменные ошибки
    │   ├── event/
    │   │   ├── event.go                    # доменное событие до сериализации envelope
    │   │   └── payload.go                  # immutable payload события
    │   ├── repository/
    │   │   └── aggregate/
    │   │       └── repository.go           # доменный порт хранилища
    │   ├── service/
    │   │   └── aggregate/
    │   │       ├── service.go              # зависимости и конструктор доменного сервиса
    │   │       ├── create.go               # одна state-changing command
    │   │       ├── get.go                  # одна query
    │   │       └── types.go                # входы и результаты сценариев
    │   └── types/
    │       ├── entity/
    │       │   └── entities.go             # сущности и агрегаты с идентичностью
    │       ├── enum/
    │       │   └── enums.go                # закрытые наборы доменных состояний
    │       ├── query/
    │       │   └── queries.go              # фильтры и read-модели домена
    │       └── value/
    │           └── values.go               # неизменяемые value objects
    ├── generated/
    │   ├── exampleservice/                  # server contract текущего сервиса
    │   ├── exampleservicecache/             # protobuf snapshot Redis cache
    │   └── dependencyservice/               # downstream Proto codegen
    ├── integration/
    │   └── provider/
    │       ├── client.go                    # адаптер внешнего provider SDK/API
    │       └── config.go                    # provider-specific config без секретных значений
    ├── maintenance/
    │   └── maintenance.go                   # bounded cleanup/reconciliation worker
    ├── observability/
    │   └── metrics/                        # только бизнесовые metrics и bounded outcomes
    ├── repository/
    │   ├── cache/
    │   │   └── aggregate/                  # Redis read-through decorator read port
    │   └── postgres/
    │       └── aggregate/
    │           ├── sql/
    │           │   ├── aggregate__get_by_id.sql
    │           │   ├── aggregate__insert.sql
    │           │   ├── command_result__get.sql
    │           │   └── command_result__insert.sql
    │           │                                # один именованный запрос на файл
    │           ├── args.go                      # преобразование домена в SQL-аргументы
    │           ├── errors.go                    # преобразование ошибок PostgreSQL
    │           ├── queries.go                   # embed и загрузка именованных запросов
    │           ├── repository.go                # реализация доменного порта
    │           ├── rows.go                      # database-only row types
    │           └── scan.go                      # преобразование строк БД в доменные типы
    └── transport/
        └── grpc/
            ├── casters/
            │   ├── requests.go                  # generated request -> domain input
            │   └── responses.go                 # domain result -> generated response
            ├── errors.go                        # доменная ошибка -> безопасный gRPC status
            └── server.go                        # реализация сгенерированного gRPC server
```

Переиспользуемые registry, Go/gRPC/HTTP/PostgreSQL/Redis metrics, OTLP runtime,
trace-aware logging и Sentry reporter находятся в `libs/go/observability`.
Переиспользуемые config, lifecycle и readiness находятся в
`libs/go/serviceruntime`, а технический HTTP endpoint - в
`libs/go/httpserver`. Переиспользуемый cache engine и protobuf codec находятся
в `libs/go/cache`; конкретный cache key, TTL и преобразование доменного снимка
принадлежат сервису. Production mTLS, unary server interceptors и
`x-correlation-id` находятся в `libs/go/grpcserver`; сервис передает
собственное отображение доменных ошибок и не копирует общий transport runtime.

Transactional outbox, provider-neutral relay, envelope, durable inbox, NATS
JetStream adapter и общие event delivery metrics находятся в
`libs/go/eventing`. Сервис владеет своей goose migration, точной конфигурацией
stream/subjects/consumer, AsyncAPI schema и consumer effect. Правила задают
`GO-DOC-004` и `GO-DOC-005`.

Подробные зависимости и назначение доменных пакетов задает `GO-DOC-001`.
PostgreSQL, именованные SQL-запросы и goose задает `GO-DOC-002`, а границы
общих модулей - `GO-DOC-006`.
`main.go` создает единственный корневой фоновый контекст и передает
`lifecycleCtx` и базовый shutdown context в `internal/app`; production-пакеты
ниже `cmd` не создают собственные `context.Background()`/`context.TODO()`.

Технический Docker/Kubernetes/Vault/NetworkPolicy каркас нового Go-компонента
создается через `tools/go-service-template`, затем переносится в канонические
пути и дополняется сервисными параметрами. Сгенерированный каркас не заменяет
domain/transport/repository реализацию и не применяется в кластер напрямую из
временного каталога.

## Внешний API-шлюз

Gateway не владеет бизнес-состоянием и не дублирует доменные правила внутренних
сервисов.

```text
services/external/example-gateway/
├── Dockerfile                              # runtime-образ gateway
├── go.mod                                  # отдельный Go module
├── go.sum
├── cmd/
│   └── example-gateway/
│       └── main.go                         # минимальная точка входа
└── internal/
    ├── app/
    │   ├── app.go                          # сборка HTTP/WebSocket server и клиентов
    │   └── config.go                       # единый env/v11 parse и валидация до запуска
    ├── clients/
    │   ├── example-service/
    │   │   ├── client.go                   # типизированный gRPC client adapter
    │   │   └── config.go                   # типизированный env/v11-фрагмент соединения
    │   └── dependency-service/
    │       ├── client.go
    │       └── config.go                   # типизированный env/v11-фрагмент соединения
    └── transport/
        ├── http/
        │   ├── generated/
        │   │   └── openapi.gen.go          # только результат OpenAPI codegen
        │   ├── casters/
        │   │   ├── requests.go             # HTTP DTO -> downstream/domain input
        │   │   └── responses.go            # downstream result -> HTTP DTO
        │   ├── errors.go                    # безопасное отображение ошибок в HTTP
        │   ├── handlers.go                  # тонкая оркестрация одного HTTP-запроса
        │   ├── middleware.go                # auth, request id, limits и observability
        │   └── router.go                    # регистрация маршрутов
        └── websocket/
            ├── generated/                   # только результат AsyncAPI codegen
            └── handlers.go                  # соединение и доставка событий
```

Исходный OpenAPI/AsyncAPI-контракт находится в `contracts/`, а не внутри
gateway. Сгенерированный код не редактируется вручную. Gateway может проверять
формат, аутентификацию, авторизацию, rate limit и идемпотентность входа, но
бизнес-решение получает от внутреннего сервиса.

## Фоновая задача

Фоновая задача является отдельным deployable в `services/jobs/<job>`, но не
становится владельцем доменных данных или правил. Она идемпотентно оркестрирует
долгую, периодическую либо повторяемую работу через контракты сервисов-владельцев.

```text
services/jobs/inventory-import-worker/
├── Dockerfile                              # воспроизводимый runtime-образ задачи
├── go.mod                                  # отдельный Go module
├── go.sum
├── cmd/
│   └── inventory-import-worker/
│       └── main.go                         # минимальная точка входа job
└── internal/
    ├── app/
    │   ├── app.go                          # lifecycle, graceful stop и wiring
    │   └── config.go                       # единый env/v11 parse и валидация до запуска
    ├── clients/
    │   ├── domain-service/
    │   │   ├── client.go                   # generated client adapter сервиса-владельца
    │   │   └── config.go                   # типизированный env/v11-фрагмент соединения
    │   └── read-model-service/
    │       ├── client.go
    │       └── config.go                   # типизированный env/v11-фрагмент соединения
    └── runner/
        └── runner.go                       # идемпотентная оркестрация одной попытки
```

Job не читает PostgreSQL другого сервиса напрямую и не копирует его бизнес-логику.
Повторы используют устойчивый operation/idempotency key, а ошибки и прогресс
доступны в наблюдаемости. Расписание Kubernetes `CronJob`, очередь или иной
триггер не меняют границу кода. Перечень и владельцев фоновых задач задает
`ARCH-DOC-004`.

## Vue/TypeScript PWA

Пример служебной PWA MatterCodex.

```text
services/staff/staff-frontend/
├── Dockerfile                              # сборка и runtime статических файлов
├── index.html                              # вход Vite
├── package.json                            # scripts и зависимости приложения
├── package-lock.json                       # единственный lockfile приложения
├── tsconfig.json                           # строгая конфигурация TypeScript
├── vite.config.ts                          # Vite, aliases и build settings
├── openapi-ts.config.ts                    # воспроизводимая генерация API client
├── env.d.ts                                # типы публичной frontend-конфигурации
├── nginx/
│   └── default.conf                        # runtime routing и security headers
└── src/
    ├── main.ts                             # bootstrap Vue
    ├── App.vue                             # корневой компонент без бизнес-логики
    ├── app/
    │   ├── router.ts                       # таблица маршрутов
    │   ├── i18n.ts                         # настройка локализации
    │   ├── plugins/                        # регистрация UI и инфраструктурных plugins
    │   └── styles/                         # глобальные tokens и базовые стили
    ├── pages/
    │   └── PartnersPage.vue                # композиция страницы из features
    ├── features/
    │   └── resources/
    │       ├── store.ts                    # Pinia state и сценарии экрана
    │       ├── model.ts                    # UI-модель, отдельная от API DTO
    │       ├── api.ts                      # feature adapter общего API client
    │       └── components/                 # компоненты только этой функции
    ├── shared/
    │   ├── api/
    │   │   ├── generated/                  # только результат OpenAPI codegen
    │   │   ├── errors.ts                   # нормализация безопасных API errors
    │   │   └── example-gateway.ts           # типизированная оболочка generated client
    │   ├── lib/                            # общие чистые helpers и constants
    │   └── ui/                             # переиспользуемые UI primitives
    └── i18n/
        ├── ru.ts                           # русские пользовательские тексты
        └── en.ts                           # английские пользовательские тексты
```

Компонент Vue не вызывает произвольный HTTP client. Цепочка имеет вид
`page/component -> feature store/api -> shared API adapter -> generated client`.
Подробные правила задает `FE-DOC-001`.

## Матрица размещения

| Вид кода                         | Каноническое место                          |
| -------------------------------- | ------------------------------------------- |
| Сущность или агрегат             | `internal/domain/types/entity`              |
| Value object                     | `internal/domain/types/value`               |
| Закрытый набор статусов          | `internal/domain/types/enum`                |
| Фильтр или read-модель           | `internal/domain/types/query`               |
| Сценарий использования           | `internal/domain/service/<capability>`      |
| Интерфейс хранилища              | `internal/domain/repository/<capability>`   |
| PostgreSQL-реализация            | `internal/repository/postgres/<capability>` |
| Клиент другого сервиса           | `internal/clients/<service>`                |
| gRPC/HTTP/WebSocket boundary     | `internal/transport/<protocol>`             |
| Конфигурация и composition root  | `internal/app`                              |
| Самостоятельная фоновая задача   | `services/jobs/<job>`                       |
| Оркестрация попытки job          | `internal/runner`                           |
| Миграция                         | `cmd/cli/migrations`                        |
| Исходный API/событийный контракт | `contracts/<format>/<component>`            |
| Generated backend code           | `internal/generated/<contract>`             |
| Generated frontend client        | `src/shared/api/generated`                  |
| UI-сценарий                      | `src/features/<feature>`                    |
| Страница PWA                     | `src/pages`                                 |
| Общий Kubernetes capability      | `deploy/k8s/base/<capability>`              |
| Манифест одного deployable       | `deploy/k8s/base/<component>`               |
| Общий технический dashboard      | `deploy/k8s/base/observability/dashboards`  |
| Бизнесовый dashboard сервиса     | `deploy/k8s/base/<service>/dashboards`      |

## Запрещенные варианты

- Плоский пакет `services/<name>/domain` вне `internal`.
- Пакеты `application` и `storage`, параллельные каноническим
  `internal/domain/service` и `internal/repository`.
- SQL-строки в production Go-коде.
- Интерфейс repository рядом с PostgreSQL-реализацией.
- Импорт transport DTO или PostgreSQL types в домен.
- Бизнес-правила в handler, gateway, `main.go`, Vue component или Pinia store.
- Ручное редактирование generated files.
- Временное дублирование старого и нового пакета с двумя источниками правды.
- Копирование общего dashboard или другого cluster-wide ресурса в service
  overlay.
- `context.Background()`/`context.TODO()` в production-коде ниже
  `cmd/<binary>/main.go`.

## Описание PR

Автор PR указывает:

1. тип каждого измененного компонента;
2. соответствующий шаблон из этого документа;
3. нарушенные зависимости, если выполняется миграция старого кода;
4. способ атомарного удаления старого пути;
5. способ ручной проверки владельцем;
6. активный профиль из `GOV-DOC-003` и фактически выполненные проверки;
7. отдельные `PASS`/`FAIL`/`NOT RUN` с причиной каждого незапущенного контура.

Структура полного unit включает поддерживаемые fixtures и test harness для
обязательных lifecycle, authorization, event и пользовательских сценариев.
Простому glue-коду тест ради покрытия не нужен; отсутствие разрешённой
disposable среды фиксируется как `NOT RUN`, а не заменяется mock readiness.

Связанные документы: `ROOT-DOC-001`, `GOV-DOC-001`, `GO-DOC-001`,
`GO-DOC-002`, `GO-DOC-005`, `GO-DOC-006`, `FE-DOC-001`, `ARCH-DOC-001`.
