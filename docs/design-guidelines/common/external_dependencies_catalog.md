# Каталог внешних зависимостей

Назначение: единая точка, где фиксируются внешние библиотеки и инструменты,
которые используются активным web-first профилем MatterCodex. Точная версия
Go- или npm-зависимости определяется соответствующим `go.mod`, `package.json` и
lock-файлом; точный image — Dockerfile либо release lock.

## Правила ведения

- Новая внешняя зависимость добавляется этим же PR.
- Удалённая из активного профиля зависимость удаляется из каталога; Git history
  является архивом.
- Generated code изменяется только штатным codegen.
- Актуальный API библиотеки проверяется через Context7 MCP. Если Context7
  недоступен, используется официальная upstream-документация и это явно
  фиксируется в PR.
- В каталоге не хранятся secret values, mutable image tags либо локаторы одного
  конкретного владельца установки.

## Backend Go

| Dependency | Active version | Scope |
|---|---:|---|
| `github.com/caarlos0/env/v11` | `v11.3.1` / `v11.4.1` | типизированная конфигурация deployable units |
| `github.com/jackc/pgx/v5` | `v5.10.0` | PostgreSQL adapters и disposable component tests |
| `github.com/pressly/goose/v3` | `v3.27.3` | embedded forward-only migrations |
| `github.com/nats-io/nats.go` | `v1.52.0` | NATS JetStream adapter за provider-neutral eventing port |
| `github.com/coder/websocket` | `v1.8.14` | owner resumable WebSocket transport |
| `github.com/mattermost/mattermost/server/public` | `v0.4.3` | только optional Mattermost interaction adapter |
| `github.com/nicksnyder/go-i18n/v2` | `v2.6.1` | embedded YAML/JSON i18n catalogs и locale-aware user messages |
| `github.com/prometheus/client_golang` | `v1.23.2` | bounded-cardinality metrics и `/metrics` |
| `github.com/getsentry/sentry-go` | `v0.48.0` | unexpected error reporting без secret/PII |
| `go.opentelemetry.io/otel` | `v1.44.0` | tracing и metrics API/SDK |
| `google.golang.org/grpc` | `v1.81.x` / `v1.82.x` | generated internal RPC transport |
| `google.golang.org/protobuf` | `v1.36.x` | generated Proto messages |
| `k8s.io/api`, `apimachinery`, `client-go` | `v0.36.3` | runtime-controller, role-image builder и exact Kubernetes resources |

Несовпадающие patch/minor версии между Go modules не нормализуются вручную:
каждый module lock является авторитетным, а общий toolchain test проверяет их
совместимость.

## Control Center

Точные версии находятся в `services/staff/control-center/package.json` и
`package-lock.json`.

| Dependency | Active version | Scope |
|---|---:|---|
| `vue` | `3.5.41` | production PWA |
| `pinia` | `3.0.4` | нормализованные authoritative entity stores |
| `vue-router` | `4.6.3` | global/project routes и guards |
| `vue-i18n` | `11.4.8` | RU/EN user interface |
| `oidc-client-ts` | `3.3.0` | browser OIDC/session boundary |
| `@hey-api/client-fetch` | `0.13.1` | generated OpenAPI client runtime |
| `@hey-api/openapi-ts` | `0.99.0` | OpenAPI TypeScript codegen |
| `@asyncapi/cli` | `6.0.2` | AsyncAPI validation/codegen |
| `vite` | `8.0.16` | production build |
| `vitest` | `4.1.6` | frontend unit tests |
| `@playwright/test` | `1.61.0` | browser E2E contract и live disposable execution |

## Agent runtime и role images

| Tool | Active version/source | Scope |
|---|---:|---|
| `@openai/codex` | `0.144.1` | первый provider adapter через typed app-server contract |
| `matter-codex-agent-runner` | текущий release digest | защищённый runtime ABI каждого role image |
| Node.js | `24.x` | Codex/provider process и role tooling |
| Go | `1.26.6` | runtime/toolchain для ролей, которым это разрешено recipe |
| `git`, `gh`, browser, office/analysis CLI | только по role recipe | инструменты конкретной роли, не обязательный core набор |

Обычный Agent запускается в exact promoted role image. Наличие GitHub,
Kubernetes, database или browser CLI определяется рецептом и grants роли, а не
глобальной IT-ориентированной базой. System assistant использует отдельный
promoted system role image и always-hot runtime contract.

## Infrastructure и supply chain

| Dependency | Source of exact version | Scope |
|---|---|---|
| rootless BuildKit | pinned Dockerfile/manifests | release images и изолированная сборка role images |
| OCI registry | release environment configuration | staging, evidence, promotion и node pull boundaries |
| Kubernetes | environment contract | платформа и execution-scoped role Pods |
| PostgreSQL | environment contract | fresh control-plane baseline и authority state |
| NATS JetStream | environment contract | durable domain-event transport |
| Vault + Secrets Store CSI | pinned manifests/charts | workload-bound secret delivery без значений в manifests |
| Keycloak/OIDC provider | environment contract | browser identity; конкретный public domain задаёт deploy owner |
| OpenTelemetry/Prometheus/Grafana | pinned manifests/release lock | telemetry и diagnostics |
| Actions Runner Controller | pinned Helm chart | release image build через owner-configured registry namespace |

Kaniko, bundled Mattermost, `bot-service`, legacy migration images,
direct-production prototype registry и hardcoded installation domain не входят
в активный профиль.

## Codegen и проверки

| Tool | Active version | Scope |
|---|---:|---|
| Go toolchain | `1.26.6` | format/build/test всех modules |
| `buf` | `v1.71.0` | Proto lint/build/codegen |
| `protoc-gen-go` | `v1.36.11` | Go Proto messages |
| `protoc-gen-go-grpc` | `v1.6.2` | Go gRPC clients/servers |
| `oapi-codegen` | `v2.7.1` | Go OpenAPI transport types |
| `sqlc` | `v1.31.1` | проверяемый SQL codegen там, где он используется |
| `govulncheck` | `v1.6.0` | Go vulnerability scan |

Версии developer tools для role images закрепляются в Dockerfile и release
lock. Их наличие в образе конкретной роли не превращает соответствующую внешнюю
систему в core dependency MatterCodex.
