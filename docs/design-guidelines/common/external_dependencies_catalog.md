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
| `base64`, `tar` | secret manifest render и image build context | подготовка Secret data и временного build context для remote image build/import без вывода секретов |
| `mmctl` | Mattermost bootstrap | локальное администрирование Mattermost pod без вывода секретов |
| `openssl` | bootstrap secrets | генерация bootstrap секретов |
| `docker` или `nerdctl` | bot-service и agent-runner image build | сборка подготовленных runtime images на MVP-контуре без registry pipeline |

## Agent runner tools - in use

| Tool | Version | Scope | Why |
|---|---:|---|---|
| `@openai/codex` | `0.138.0` | Codex developer/reviewer agent | `codex exec --json`, MCP config smoke и non-interactive developer/reviewer run внутри Kubernetes Job |
| `github-cli` / `gh` | distro package | Agent PR publish/review | подготовленный agent-runner image вызывает `gh` из Go runner binary, а Codex agent получает `gh` для inline review comments и review-thread replies |
| `git` | distro package | Agent checkout/push | подготовленный agent-runner image выполняет clone/branch/commit/push из Go runner binary без shell-скриптов в bot-service |

## Runtime images - in use

| Image | Scope | Why |
|---|---|---|
| `golang:1.26-alpine` | Go build stages | build layer для bot-service и agent-runner binaries; не используется как production runtime |
| `alpine:3.22` | bot-service prod Dockerfile | минимальный runtime слой для собранного bot-service binary |
| `matter-codex-bot-service:dev` | bot-service MVP runtime | локально/удаленно собранный image с готовым `bot-service` binary |
| `node:22-alpine` | agent-runner Dockerfile base | runtime слой с npm для установки Codex CLI при сборке подготовленного agent-runner image |
| `matter-codex-agent-runner:dev` | agent runner MVP runtime | локально собранный image с `matter-codex-agent-runner`, `gh`, `git` и Codex CLI для smoke/developer/reviewer/auth Job |
| `quay.io/oauth2-proxy/oauth2-proxy` | Mattermost public gate | GitHub OAuth allowlist перед публичным Mattermost URL без встраивания OAuth-логики в Mattermost manifests |
| `mattermost/mattermost-team-edition` | Mattermost | self-hosted Mattermost для control surface |
| `postgres:16-alpine` | Mattermost PostgreSQL | single-server MVP БД Mattermost |
| `busybox` | init/wait helpers | lightweight init helper в manifests; legacy smoke image setting сохраняется для совместимости config |

## Процесс изменений каталога

- PR с новой зависимостью должен обновлять этот файл, `go.mod`/lock-файлы и профильные гайды при необходимости.
- Без обновления каталога изменение зависимости считается неполным.
