# Архитектура проекта

Цель: предсказуемое развитие `matter-codex` как Mattermost-first платформы управления Codex-агентами, задачами, runtime и delivery-процессами в Kubernetes.

База: DDD (bounded contexts) + Clean Architecture (зависимости снаружи внутрь) + явные deploy contracts + единый каркас директорий.

## Архитектурные ограничения matter-codex

- Оркестратор: только Kubernetes.
- Control surface MVP: Mattermost bot-service, slash commands, team/channels.
- Agent runtime: отдельный pod на задачу, собственный PVC, checkout рабочей ветки внутри PVC.
- Интеграция с Kubernetes: Go SDK (`client-go`) через интерфейсы и адаптеры в runtime-коде.
- Интеграция с Mattermost: официальный/public Mattermost SDK и typed модели там, где они доступны.
- Интеграция с репозиториями: интерфейсы провайдеров (`github` сейчас, `gitlab` позже).
- Процессы: event/webhook-driven, без workflow-first модели.
- Хранилище и синхронизация multi-pod: PostgreSQL, `JSONB`, будущий `pgvector`.
- Shell допустим для короткого bootstrap/deploy MVP, но не как место доменной логики или долгоживущей orchestration.

## Структура репозитория

Целевой верхний уровень:

- `services/` - Go-сервисы по зонам `internal|external|jobs|dev`.
- `libs/` - переиспользуемый Go-код, создаётся только при наличии реального переиспользования.
- `proto/` - gRPC контракты, создаётся при появлении внутренних gRPC API.
- `specs/` - OpenAPI/AsyncAPI контракты, создаётся при появлении стабильных транспортных контрактов.
- `deploy/` - Kubernetes manifests/templates.
- `scripts/` - временные bootstrap/deploy/smoke wrappers для MVP.
- `docs/` - документация, решения и runbooks.
- `tools/` - Go tooling, codegen и deploy/render utilities по мере развития.

Текущий MVP использует `deploy/k8s/**` и `scripts/**`. Целевое направление - постепенно переносить render/apply/reconcile логику в Go-инструменты, не блокируя быстрые проверяемые PR.

## Agent runner и внешние CLI

Agent runner image содержит нужные инструменты (`codex`, `gh`, `git`, language toolchains) заранее. Runtime Go-код оркестрирует их прямыми вызовами `exec.CommandContext` на границе runner/adapter и передаёт аргументы списком, без shell interpolation.

Допустимо:

- вызвать готовый CLI (`codex`, `gh`, `git`) с явным списком аргументов;
- подготовить env/file mounts для аккаунтов OpenAI/GitHub через Kubernetes Secret;
- передать агенту рабочие инструкции через prompt template из БД.

Недопустимо:

- хранить workflow агента как `sh -c`, `bash -c` или многострочный shell-сценарий в Go-коде;
- устанавливать runtime tools из Go/shell во время agent run вместо сборки нормального образа;
- зашивать правила PR/review/ответов на comments в Go-строки, если это часть профиля агента и должно редактироваться через Mattermost.

## Deploy templates и shell wrappers

Kubernetes object definitions живут только в YAML templates под `deploy/**`. Shell-скрипты в `scripts/**` являются тонкой обвязкой: читают `.env`, вычисляют значения для render, вызывают `mattercodex_render_template`, применяют уже отрендеренный файл и выполняют readback/smoke команды.

Допустимо в shell:

- вычислять secret data в переменных вида `*_B64` без вывода значений;
- выбирать dry-run/apply режим;
- рендерить `deploy/**/*.yaml.tpl` в файл или поток;
- передавать уже отрендеренный YAML в `kubectl apply` локально или по SSH.

Недопустимо в shell:

- держать `apiVersion`, `kind`, `metadata`, `spec` или другие части Kubernetes manifests в heredoc;
- генерировать Kubernetes object через `kubectl create ... -o yaml` как основной template path;
- смешивать в одном shell-блоке бизнес-решение, secret preparation и YAML object definition;
- добавлять новый deploy object без соответствующего файла `deploy/k8s/**.yaml.tpl`.

## Зоны сервисов

### `services/external/`

- Публичные webhook/API точки входа.
- Mattermost slash command callbacks, GitHub webhooks, внешние callback handlers.
- Валидация, authn/authz, rate limiting, аудит.
- Без доменной orchestration внутри transport слоя.

### `services/internal/`

- Доменные правила платформы.
- Работа с БД, Kubernetes и repository providers через интерфейсы.
- Нет публичного ingress для бизнес-эндпоинтов.

### `services/jobs/`

- Async/фоновые процессы: reconciliation, ретраи, cleanups, indexing, agent run watchers.
- Идемпотентность и устойчивость обязательны.

### `services/dev/`

- Только dev-инструменты.
- Не деплоятся в production.

## Dockerfile deployable-сервиса

Каждый deployable Go-сервис должен иметь Dockerfile рядом с сервисом: `services/<zone>/<service>/Dockerfile`.

Минимальные требования:

- стадии `build`, `dev`, `prod`;
- `build` собирает бинарник из `cmd/<service>/main.go`;
- `dev` подходит для локального/slot запуска и не является production runtime;
- `prod` запускает готовый бинарник, не зависит от исходников и инструментов разработки;
- production Deployment использует собранный image и entrypoint сервиса; исходники не передаются через ConfigMap/Secret, `go run` в pod не используется;
- порты объявляются в Kubernetes manifests и runtime config, а не через обязательный `EXPOSE`;
- Kubernetes manifests выбирают runtime явно и не скрывают обязательные env/secrets.

## Границы ответственности

- Один сервис = один bounded context или один edge-контур.
- Домен не зависит от HTTP, Mattermost, Kubernetes, GitHub или PostgreSQL SDK напрямую.
- SDK details изолируются в transport/infrastructure adapters.
- Shared DB без владельца контекста запрещён; таблицы и данные имеют явного владельца.
- Транспортные DTO не используются как domain entities.

## Схема взаимодействия

- `external/*` принимает внешние события, валидирует вход и маршрутизирует запросы.
- `internal/*` владеет доменной логикой и каноническим состоянием.
- `jobs/*` выполняет фоновые и долгие процессы идемпотентно.
- Интеграции с Kubernetes, Mattermost и repository providers идут через интерфейсы и адаптеры.
