# External Dependencies Catalog

Назначение: единая точка, где фиксируются внешние библиотеки и инструменты, разрешённые или уже используемые в `matter-codex`.

## Правила ведения

- Новая внешняя зависимость добавляется в этот каталог тем же PR, который начинает её использовать.
- Для Go зависимости версия фиксируется в `go.mod`.
- Если зависимость удалена, запись переводится в `deprecated` с датой или удаляется в PR, где удалён весь её usage.
- Для актуальных API библиотек перед изменением кода используется Context7 MCP или официальный upstream-документ.

## Backend Go - in use

| Dependency | Version | Scope | Why |
|---|---:|---|---|
| `github.com/caarlos0/env/v11` | `v11.3.1` | Config | typed env -> struct parsing без самописного env loader в сервисе |
| `github.com/google/go-github/v88` | `v88.0.0` | GitHub SDK | repository access, branch/PR operations и webhook payload helpers без ручной REST-обвязки |
| `github.com/jackc/pgx/v5` | `v5.10.0` | PostgreSQL | storage repositories через `pgxpool`; `stdlib` driver для goose |
| `github.com/mattermost/mattermost/server/public` | `v0.4.2` | Mattermost SDK/model | typed `CommandResponse` и публичные модели Mattermost вместо ручных JSON-структур |
| `github.com/modelcontextprotocol/go-sdk` | `v1.2.0` | MCP SDK | встроенный Streamable HTTP MCP server для ограниченного чтения/записи Mattermost thread context агентами |
| `github.com/nicksnyder/go-i18n/v2` | `v2.6.1` | i18n | runtime `libs/go/i18n` для embedded JSON message catalogs, template variables и locale switching |
| `github.com/pressly/goose/v3` | `v3.27.1` | PostgreSQL migrations | embedded SQL migrations с `-- +goose Up/Down` вместо самописного migration runner |
| `github.com/prometheus/client_golang` | `v1.23.2` | Observability | `/metrics`, Go/process collectors и Prometheus HTTP handler |
| `k8s.io/api` | `v0.36.1` | Kubernetes typed API | typed `batch/v1` Job, `core/v1` Pod/PVC и `PodLogOptions` для runtime adapter |
| `k8s.io/apimachinery` | `v0.36.1` | Kubernetes API machinery | typed meta/options, labels, resource quantities и Kubernetes API errors |
| `k8s.io/client-go` | `v0.36.1` | Kubernetes SDK | in-cluster/kubeconfig client, Job/PVC/Secret operations, pod status/log tail и `remotecommand` exec для Codex auth handoff без shell-first runtime |

## Backend Go - planned baselines

| Dependency | Status | Scope | Why |
|---|---|---|---|
| `github.com/openai/openai-go/v3` | planned | Agent/OpenAI integration | официальный OpenAI Go SDK для будущего runtime-контура |

## Infrastructure and bootstrap tools - in use

| Tool | Scope | Why |
|---|---|---|
| `ssh` | remote deploy wrapper | выполнение Kubernetes операций непосредственно на целевом сервере |
| `kubectl` | bootstrap/deploy wrapper | применение manifests и rollout/smoke diagnostics в MVP |
| `envsubst` | manifest render | шаблонизация YAML до появления Go deploy renderer |
| `base64`, `tar` | secret manifest render и image build context | подготовка Secret data и временного build context для Kaniko remote image build без вывода секретов |
| `mmctl` | Mattermost bootstrap | локальное администрирование Mattermost pod без вывода секретов |
| `openssl` | bootstrap secrets | генерация bootstrap секретов |
| `gcr.io/kaniko-project/executor` | bot-service и agent-runner image build | default in-cluster image build без Docker daemon и без передачи готовых image с локальной машины |
| `registry:2` | MatterCodex image registry | single-server локальный registry для Kaniko push и kubelet pull через hostPort |
| `docker` или `nerdctl` | legacy remote image build | явный fallback только при `MATTERCODEX_IMAGE_BUILD_STRATEGY=docker` и наличии builder на целевом сервере |

## Agent runner tools - in use

| Tool | Version | Scope | Why |
|---|---:|---|---|
| `@openai/codex` | `0.144.1` | Codex developer/reviewer agent | `codex exec --json`, MCP config smoke и non-interactive developer/reviewer run внутри Kubernetes Job |
| `node` | `24.17.x` | Agent JS/TS runtime | запуск Vue/TypeScript/OpenAPI/AsyncAPI tooling; свежий `@asyncapi/cli` требует Node 24 |
| `npm` | `11.13.x` | Agent JS package runner | запуск npm scripts и глобальных CLI packages |
| `pnpm` | `11.8.0` | Agent JS package runner | поддержка frontend/workspace проектов на pnpm |
| `yarn` | `1.22.22` | Agent JS package runner | поддержка проектов на Yarn classic |
| `gh` | `2.95.0` | Agent PR publish/review | подготовленный agent-runner image вызывает `gh` из Go runner binary, а Codex agent получает `gh` для inline review comments и review-thread replies |
| `git` | distro package | Agent checkout/push | подготовленный agent-runner image выполняет clone/branch/commit/push из Go runner binary без shell-скриптов в bot-service |
| `tini` | distro package | Agent container init | PID 1 init для agent-runner pods/jobs; reaps orphaned/zombie child processes от `codex`, `gh`, `git`, `npm` и прокидывает сигналы |
| `kubectl` | `1.36.2` | Agent Kubernetes diagnostics/deploy | роли с Kubernetes-доступом могут читать логи, проверять ресурсы и выполнять deploy через Kubernetes CLI |
| `helm` | `4.2.1` | Agent Kubernetes diagnostics/deploy | inspect/render Helm releases and charts |
| `psql` | distro package | Agent PostgreSQL diagnostics | диагностика PostgreSQL и ручная проверка данных по разрешению владельца |
| `redis-cli` | distro package | Agent Redis diagnostics | диагностика Redis/cache состояния по разрешению владельца |
| `jq` | distro package | Agent diagnostics/scripts | безопасная обработка JSON-выводов CLI без ad-hoc parsing |
| `yq` | `v4.53.3` | Agent YAML diagnostics/scripts | обработка YAML manifests/config без строкового парсинга |
| `rg`, `fd`, `nc`, `dig`, `tree` | distro packages | Agent development diagnostics | быстрый поиск, network/DNS diagnostics и обзор рабочих деревьев |
| `just` | `1.55.1` | Agent task runner | запуск project tasks из justfile через pinned release binary, чтобы не зависеть от distro package availability |
| `go` | `1.26` | Agent Go development | сборка и тестирование Go modules; Go 1.26 выбран, потому что свежий `sqlc` требует Go >= 1.26, при этом Go 1.25 modules проекта `kodex` остаются совместимыми |
| `goimports` | `v0.46.0` | Agent Go formatting | форматирование импортов Go |
| `gofumpt` | `v0.10.0` | Agent Go formatting | stricter Go formatting where requested |
| `staticcheck` | `v0.7.0` | Agent Go static analysis | дополнительные проверки Go-кода |
| `goose` | `v3.27.1` | Agent migration work | запуск и проверка `-- +goose Up/Down` миграций в Go/PostgreSQL сервисах |
| `sqlc` | `v1.31.1` | Agent SQL codegen | генерация typed Go database code из SQL |
| `mockgen` | `v0.6.0` | Agent test codegen | генерация Go mocks |
| `oapi-codegen` | `v2.7.1` | Agent OpenAPI codegen | генерация Go transport-кода из OpenAPI спецификаций |
| `openapi-ts` | `0.98.2` | Agent OpenAPI TypeScript codegen | генерация TypeScript clients из OpenAPI спецификаций |
| `typescript` / `tsc` | `6.0.3` | Agent TypeScript development | type-check TypeScript фронтенда и shared packages |
| `vue` | `3.5.38` | Agent Vue development | runtime package baseline для Vue/PWA проектов |
| `create-vue` | `3.22.4` | Agent Vue scaffolding | создание Vue packages при необходимости |
| `vite` | `8.0.16` | Agent Vue build/dev server | сборка и локальная проверка Vue/Vite интерфейсов |
| `vue-tsc` | `3.3.5` | Agent Vue typecheck | type-check Vue SFC |
| `vitest` | `4.1.9` | Agent frontend tests | запуск unit tests для frontend packages |
| `eslint` | `10.5.0` | Agent frontend lint | lint JavaScript/TypeScript |
| `prettier` | `3.8.4` | Agent formatting | форматирование frontend/docs files |
| `asyncapi` | `6.0.2` | Agent AsyncAPI/WebSocket codegen | валидация AsyncAPI specs и запуск generators для event/websocket contracts |
| `@asyncapi/generator` | `3.3.0` | Agent AsyncAPI codegen | generator runtime package для AsyncAPI templates |
| `modelina` | `5.10.1` | Agent AsyncAPI model codegen | генерация TypeScript models для AsyncAPI/WebSocket payloads |
| `wscat` | `6.1.0` | Agent WebSocket diagnostics | ручная проверка websocket endpoints |
| `chromium` | distro package | Agent browser diagnostics | системный browser binary для диагностики и версионных проверок; основной путь UI smoke/e2e в agent pod - Playwright CLI/API |
| `playwright` / `@playwright/test` | `1.61.1` | Agent browser smoke/e2e | browser automation, screenshots, traces и e2e/smoke проверки developer, UI/UX и QA ролей |
| `@playwright/mcp` | `0.0.77` | Optional agent browser MCP | MCP-сервер браузерной автоматизации для ролей, которым он явно включен в Codex `config.toml` |
| `wait-on` | `9.0.10` | Agent frontend readiness | ожидание локального dev-server/build preview перед browser smoke/e2e |
| `buf` | `v1.71.0` | Agent protobuf/gRPC codegen | lint/generate protobuf contracts |
| `grpcurl` | `v1.9.3` | Agent gRPC diagnostics | инспекция и вызов gRPC сервисов |
| `protoc` | distro package | Agent protobuf/gRPC codegen | генерация protobuf/gRPC артефактов |
| `protoc-gen-go` | `v1.36.11` | Agent protobuf Go codegen | генерация Go protobuf типов |
| `protoc-gen-go-grpc` | `v1.6.2` | Agent gRPC Go codegen | генерация Go gRPC server/client stubs |
| `golangci-lint` | `v2.12.2` | Agent Go lint | запуск основного Go lint профиля, когда это требуется задачей |

## Runtime images - in use

| Image | Scope | Why |
|---|---|---|
| `golang:1.26-alpine` | Go build stages | build layer для bot-service и agent-runner binaries; не используется как production runtime |
| `golang:1.26-alpine` | agent-runner Go toolchain/tools stage | поставляет Go 1.26 и Go CLI tools в agent-runner runtime для свежего codegen/lint toolchain |
| `alpine:3.22` | bot-service prod Dockerfile | минимальный runtime слой для собранного bot-service binary |
| `localhost:5001/matter-codex/bot-service:<tag>` | bot-service MVP runtime | image, собранный Kaniko в кластере и опубликованный во встроенный MatterCodex registry |
| `node:24-bookworm` | agent-runner Dockerfile base | glibc runtime слой с npm, Codex CLI, Vue/TS/OpenAPI/AsyncAPI tooling, operator/developer CLI tools и Playwright/Chromium browser tooling |
| `localhost:5001/matter-codex/agent-runner:<tag>` | agent runner MVP runtime | non-root image, собранный Kaniko в кластере, с `matter-codex-agent-runner`, Codex CLI, GitHub/Kubernetes/DB/WebSocket clients, Go toolchain, Vue/TS и API codegen tooling для chat/session agents |
| `quay.io/oauth2-proxy/oauth2-proxy` | Mattermost public gate | Google OAuth allowlist перед публичным Mattermost URL без встраивания OAuth-логики в Mattermost manifests |
| `mattermost/mattermost-team-edition` | Mattermost | self-hosted Mattermost для control surface |
| `postgres:16-alpine` | Mattermost PostgreSQL | single-server MVP БД Mattermost |
| `busybox` | init/wait helpers | lightweight init helper в manifests; legacy smoke image setting сохраняется для совместимости config |

## Процесс изменений каталога

- PR с новой зависимостью должен обновлять этот файл, `go.mod`/lock-файлы и профильные гайды при необходимости.
- Без обновления каталога изменение зависимости считается неполным.
