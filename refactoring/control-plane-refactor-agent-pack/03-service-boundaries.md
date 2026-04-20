# Границы сервисов и ownership

## Общие правила границ

У каждого сервиса должны быть:
- свой каталог кода;
- свой proto package;
- своя база данных;
- свой каталог миграций;
- свой README;
- свой owner в команде;
- свой список SLO и operational метрик.

## Каталог сервисов

### 1. Platform Admin & IAM Service

**Ответственность**
- users
- staff auth
- OAuth / allowlist / principal resolution
- access checks
- system settings
- audit trail административных изменений

**Предлагаемое владение данными**
- `users`
- таблицы, связанные с auth и allowlist
- `system_settings`
- `system_setting_changes`
- при необходимости `project_members`, если будет решено, что membership - это access-domain

**Кто должен ходить в сервис**
- Project Catalog Service для access checks
- Run Orchestrator Service для principal и staff resolution
- Interaction Service, если нужны данные по человеку или access policy

**Что нельзя оставлять в этом сервисе**
- project configuration
- runtime deploy logic
- interactions workflow
- mission control projections

### 2. Project Catalog Service

**Ответственность**
- projects
- repositories
- repository metadata
- config entries
- project/repository/platform tokens, если они служат для интеграций каталога
- webhook setup и preflight вокруг репозиториев

**Предлагаемое владение данными**
- `projects`
- `repositories`
- `config_entries`
- `platform_github_tokens`
- `project_github_tokens`
- связанная repo config metadata

**Внешние потребители**
- Run Orchestrator Service
- Runtime Deploy Service
- Mission Control Service

**Ключевой принцип**
Никто не читает `repositories` и `config_entries` из БД напрямую. Все вопросы о проекте и репозитории идут в Project Catalog Service.

### 3. Run Orchestrator Service

**Ответственность**
- ingest GitHub webhooks
- создание и изменение runs
- flow events
- agent sessions
- status transitions
- wait states и resume payload
- GitHub rate limit waits
- runtime errors, если они входят в lifecycle run

**Предлагаемое владение данными**
- `agent_runs`
- `flow_events`
- `agent_sessions`
- `github_rate_limit_waits`
- `github_rate_limit_wait_evidence`
- `runtime_errors` при подтверждении связи с lifecycle run

**Внешние потребители**
- Runtime Deploy Service
- Interaction Service
- Mission Control Service
- workers / agent-runner / внешние edge-компоненты

**Ключевой принцип**
Run status и run lifecycle являются source of truth только здесь.

### 4. Runtime Deploy Service

**Ответственность**
- prepare environment
- runtime reuse evaluation
- deploy tasks
- leasing
- cancel/stop semantics
- Kubernetes orchestration
- registry operations для runtime-окружения

**Предлагаемое владение данными**
- `runtime_deploy_tasks`
- дополнительные deploy-specific tables и outbox

**Внешние потребители**
- Run Orchestrator Service
- workers

**Ключевой принцип**
Этот сервис не должен знать структуру чужих таблиц. Он работает через свои tasks и gRPC вызовы к owner-сервисам.

### 5. Interaction Service

**Ответственность**
- interaction requests
- delivery attempts
- callback events
- effective responses
- timeout/expiry
- Telegram или другие adapters

**Предлагаемое владение данными**
- `interaction_requests`
- `interaction_delivery_attempts`
- `interaction_callback_events`
- `interaction_response_records`
- `interaction_channel_bindings`
- `interaction_callback_handles`

**Внешние потребители**
- Run Orchestrator Service
- workers
- внешние callback endpoints

**Ключевой принцип**
Весь workflow запроса человеческого решения и возврата ответа живет здесь целиком.

### 6. Mission Control Service

**Ответственность**
- workspace / dashboard / graph
- timeline / relations / snapshots
- command queue и leasing
- governance projections и решения

**Предлагаемое владение данными**
- `mission_control_*`
- `change_governance_*`

**Внешние потребители**
- UI / staff tools
- workers для command execution
- Run Orchestrator Service для статусов и событий

**Ключевой принцип**
Read models, projections и governance artifacts не должны жить рядом с run orchestration или project catalog.

## Что считать нарушением boundaries

Нарушение границ - это любой из случаев:
- сервис использует SQL к чужой БД;
- сервис импортирует чужой internal-repository package ради чтения таблиц;
- в proto одного сервиса начинают просачиваться DTO другого домена;
- один PR переносит только transport, но оставляет бизнес-логику и данные в старом месте;
- ownership таблицы не определен явно в документации.

## Решение спорных случаев

Если ownership таблицы или use case спорный, агент обязан:
- завести ADR;
- зафиксировать выбранный owner-сервис;
- перечислить аргументы "почему не в соседнем сервисе";
- обновить `03-service-boundaries.md` в том же PR.
