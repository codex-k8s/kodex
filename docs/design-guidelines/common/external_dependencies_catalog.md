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
| `github.com/jackc/pgx/v5` | `v5.10.0` | PostgreSQL | storage migrations/repositories через `pgxpool` без database/sql wrapper |
| `github.com/mattermost/mattermost/server/public` | `v0.4.2` | Mattermost SDK/model | typed `CommandResponse` и публичные модели Mattermost вместо ручных JSON-структур |
| `github.com/prometheus/client_golang` | `v1.23.2` | Observability | `/metrics`, Go/process collectors и Prometheus HTTP handler |

## Backend Go - planned baselines

| Dependency | Status | Scope | Why |
|---|---|---|---|
| `k8s.io/client-go` | planned | Kubernetes integration | запуск agent pod/job, PVC, watcher/status через Kubernetes SDK |
| `github.com/google/go-github/v82` | planned | Repository provider | GitHub provider adapter без ручной REST-обвязки |
| `github.com/openai/openai-go/v3` | planned | Agent/OpenAI integration | официальный OpenAI Go SDK для будущего runtime-контура |

## Infrastructure and bootstrap tools - in use

| Tool | Scope | Why |
|---|---|---|
| `ssh` | remote deploy wrapper | выполнение Kubernetes операций непосредственно на целевом сервере |
| `kubectl` | bootstrap/deploy wrapper | применение manifests и rollout/smoke diagnostics в MVP |
| `envsubst` | manifest render | шаблонизация YAML до появления Go deploy renderer |
| `base64`, `tar` | source ConfigMap render | временная упаковка Go source для быстрого MVP deploy без CI image pipeline |
| `mmctl` | Mattermost bootstrap | локальное администрирование Mattermost pod без вывода секретов |
| `openssl` | bootstrap secrets | генерация bootstrap секретов |

## Runtime images - in use

| Image | Scope | Why |
|---|---|---|
| `golang:1.26-alpine` | bot-service MVP runtime | быстрый запуск Go-сервиса из source ConfigMap до image pipeline |
| `alpine:3.22` | bot-service prod Dockerfile | минимальный runtime слой для будущей сборки образа |
| `mattermost/mattermost-team-edition` | Mattermost | self-hosted Mattermost для control surface |
| `postgres:16-alpine` | Mattermost PostgreSQL | single-server MVP БД Mattermost |
| `busybox` | init/wait helpers | lightweight init helper в manifests |

## Процесс изменений каталога

- PR с новой зависимостью должен обновлять этот файл, `go.mod`/lock-файлы и профильные гайды при необходимости.
- Без обновления каталога изменение зависимости считается неполным.
