# External Dependencies Catalog

Назначение: единая точка, где фиксируются внешние библиотеки и инструменты,
разрешённые/используемые в `kodex`.

## Правила ведения

- Любая новая внешняя зависимость сначала добавляется в этот каталог.
- Для каждой зависимости фиксируются:
  - где используется;
  - зачем нужна;
  - есть ли альтернатива;
  - кто владелец решения (роль/команда).
- Для Go зависимости версия фиксируется в `go.mod`; для JS/Vue — в `package.json`.
- Если зависимость удалена, запись не удаляется молча, а переводится в `deprecated` с датой.

## Backend (Go) — in use

| Dependency | Version | Scope | Why |
|---|---|---|---|
| `github.com/labstack/echo/v5` | `v5.0.3` | HTTP transport | единый REST стек для gateway/staff API |
| `github.com/getkin/kin-openapi` | `v0.133.0` | OpenAPI validation | загрузка/валидация OpenAPI и runtime request-validation в gateway-сервисах |
| `github.com/oapi-codegen/runtime` | `v1.1.2` | OpenAPI generated transport runtime | типы/утилиты для сгенерированного OpenAPI Go-кода |
| `github.com/prometheus/client_golang` | `v1.23.2` | Observability | `/metrics` и базовые метрики сервиса |
| `github.com/jackc/pgx/v5` | `v5.9.2` | PostgreSQL driver | доступ к PostgreSQL |
| `github.com/google/uuid` | `v1.6.0` | Utility, PostgreSQL helpers | генерация и передача идентификаторов, включая общие helpers в `libs/go/postgres` |
| `github.com/caarlos0/env/v11` | `v11.3.1` | Config | типобезопасный env->struct парсинг конфигурации |
| `github.com/nicksnyder/go-i18n/v2` | `v2.6.1` | Backend i18n | локализация системных message id через общий runtime `libs/go/i18n` |
| `github.com/golang-jwt/jwt/v5` | `v5.3.0` | Auth | выпуск и валидация short-lived JWT для staff API |
| `golang.org/x/crypto` | `v0.47.0` | Security | sealed-box шифрование значений для GitHub repository secrets (`CreateOrUpdateRepoSecret`) |
| `k8s.io/client-go` | `v0.35.0` | Kubernetes integration | проверка связности кластера через discovery API и запуск/проверка Job через Kubernetes SDK |
| `k8s.io/api` | `v0.35.0` | Kubernetes API types | типы Kubernetes для Job/Pod и будущих расширенных проверок кластера |
| `k8s.io/apimachinery` | `v0.35.0` | Kubernetes API machinery | ошибки API, meta types, утилиты client-go |
| `github.com/google/go-github/v82` | `v82.0.0` | Repository provider (GitHub) | настройка вебхуков и валидация доступа к репозиториям через GitHub API v3 |
| `github.com/google/go-querystring` | `v1.2.0` | Dependency of go-github | сериализация query params для GitHub API клиента |
| `github.com/hashicorp/vault/api` | `v1.23.0` | Secret resolver | официальный Go SDK для чтения Vault KV v2 в `libs/go/secretresolver` без самописного Vault API клиента |
| `google.golang.org/grpc` | `v1.79.3` | Internal transport | внутреннее service-to-service взаимодействие (`gateway` -> внутренний сервис и service-to-service) |
| `go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc` | `v0.67.0` | Наблюдаемость внутреннего gRPC | трассировка и метрики gRPC-сервера через `grpc.StatsHandler`; версия выбрана без подъёма `google.golang.org/grpc` выше `v1.79.3` |
| `go.opentelemetry.io/otel` | `v1.42.0` | API наблюдаемости | базовые OpenTelemetry API и W3C-схема проброса контекста для общих runtime-библиотек |
| `go.opentelemetry.io/otel/metric` | `v1.42.0` | API метрик | типы `MeterProvider` для настройки gRPC-инструментации без привязки сервиса к конкретному экспортёру |
| `go.opentelemetry.io/otel/trace` | `v1.42.0` | API трассировки | типы `TracerProvider` и trace context для настройки gRPC-инструментации без привязки сервиса к конкретному экспортёру |
| `go.opentelemetry.io/otel/sdk` | `v1.42.0` | Тесты наблюдаемости и будущая SDK-инициализация | SDK-провайдеры для проверки проброса trace context; экспортёр и OTel Collector боевого контура подключаются отдельным срезом начальной настройки |
| `google.golang.org/protobuf` | `v1.36.11` | Internal contracts | protobuf runtime для gRPC контрактов и сгенерированного кода в `proto/gen/go/**` |
| `google.golang.org/genproto/googleapis/rpc` | `v0.0.0-20260406210006-6f92a3bedf2d` | gRPC error details | типизированные `errdetails` для conflict/status metadata во внутренних gRPC callback-ах |
| `go.yaml.in/yaml/v2` | `v2.4.3` | Contract tests | парсинг YAML-спецификаций в Go contract tests без строкового поиска по контракту |
| `github.com/modelcontextprotocol/go-sdk` | `v1.3.0` | MCP transport | встроенный StreamableHTTP MCP transport/auth/resource/tool runtime для платформенного MCP-сервера |
| `github.com/openai/openai-go/v3` | `v3.28.0` | Sprint S11 Telegram adapter voice STT | официальный OpenAI Go SDK для speech-to-text в `telegram-interaction-adapter`; используется для voice reply transcription после `ffmpeg` normalization |

## Backend (Go) — planned baselines

| Dependency | Version | Scope | Why |
|---|---|---|---|
| `github.com/mymmrac/telego` | `v1.7.0` | Sprint S11 Telegram adapter (`adopted` в `go.mod`) | pragmatic Telegram Bot API SDK baseline для webhook mode, inline keyboards и callback queries; используется в platform-owned Telegram adapter contour для webhook/auth, callback acknowledgement и Bot API mediation |

## Frontend (Vue/TS) — in use

| Dependency | Status | Scope | Why |
|---|---|---|---|
| `vue` | in use (package.json, `^3.5.25`) | UI framework | staff web-console |
| `typescript` | in use (devDependency, `~5.9.3`) | Типизация | строгая типизация staff web-console |
| `vite` | in use (devDependency, `^6.4.2`) | Сборка | Vite dev server и production-сборка staff web-console на Node 18+ |
| `@vitejs/plugin-vue` | in use (devDependency, `^5.2.4`) | Сборка | поддержка Vue SFC в Vite |
| `vue-tsc` | in use (devDependency, `^2.2.12`) | Проверка типов | проверка Vue SFC и TypeScript без emit |
| `vue-router` | in use (package.json, `^4.6.3`) | Маршрутизация | маршрутизация staff UI |
| `pinia` | in use (package.json, `^3.0.4`) | Состояние | минимальное состояние UI |
| `axios` | in use (package.json, `^1.13.2`) | HTTP-клиент | вызовы staff/private API через сгенерированный OpenAPI-клиент |
| `vue-i18n` | in use (package.json, `^9.14.5`) | i18n | все пользовательские тексты через i18n ключи; версия выбрана для текущего Node 18-контура |
| `vue3-cookies` | in use (package.json, `^1.0.6`) | Cookies | будущий единый cookie-адаптер для UI-настроек, без хранения секретов |
| `date-fns` | in use (package.json, `^4.1.0`) | Datetime formatting | безопасное форматирование дат/времени без самописных helpers |
| `vuetify` | in use (package.json, `^3.11.8`) | UI-компоненты | единая UI-библиотека и каркас приложения для staff web-console |
| `vite-plugin-vuetify` | in use (devDependency, `^2.1.3`) | Сборка | Vite-интеграция Vuetify (auto-import/стили) |
| `sass` | in use (devDependency, `1.69.7`) | Сборка | сборка Vuetify styles (Sass) в Vite на Node 18+ |
| `@mdi/font` | in use (package.json, `^7.4.47`) | Icons | базовый icon font для Vuetify (Material Design Icons) |
| `monaco-editor` | planned | Editor | markdown и YAML редакторы для будущих экранов staff web-console; первый MVP не подключает editor dependency |
| `@hey-api/openapi-ts` | in use (devDependency, `v0.80.0`) | OpenAPI codegen (TS) | генерация типизированного API-клиента для frontend из `specs/openapi/staff-gateway.v1.yaml` на Node 18+ |
| `@hey-api/client-axios` | deprecated (bundled in `@hey-api/openapi-ts` since `v0.73.0`) | OpenAPI axios client plugin | отдельная установка не требуется, использовать встроенный плагин через конфиг `openapi-ts` |

## Infrastructure and CI tools — in use

| Tool | Scope | Why |
|---|---|---|
| `gh` CLI | operator diagnostics | ручная проверка GitHub run/status/logs в troubleshooting сценариях |
| `kubectl` | operator diagnostics | ручная диагностика k8s ресурсов и логов вне runtime API |
| `openssl` | bootstrap scripts | генерация секретов |
| `kaniko` | CI build pipeline | сборка образа внутри кластера |
| `node` image | frontend build pipeline | сборка Vite bundle для `web-console` внутри Kaniko job; версия фиксируется в `services.yaml` как `node-alpine` |
| `nginxinc/nginx-unprivileged` image | frontend runtime | non-root runtime для отдачи static bundle `web-console`; версия фиксируется в `services.yaml` как `nginx-unprivileged` |
| `goose` (`v3.26.0`) | запуск миграций БД | применение `-- +goose Up/Down` миграций в production-задании миграций |
| `@openai/codex` (CLI) | `services/jobs/agent-runner` runtime | выполнение `codex exec`/`resume` в агентном Job-контуре Day4 |
| `github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen` | Make codegen pipeline | генерация Go transport-артефактов из OpenAPI |

## Процесс изменений каталога

- PR с новой зависимостью должен обновлять:
  - этот файл;
  - релевантный гайд (`go/libraries.md`, `vue/libraries.md` и т.п.);
  - технические артефакты (`go.mod`, `package.json`, workflow/bootstrap при необходимости).
- Без обновления каталога изменение считается неполным.
