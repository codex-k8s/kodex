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
