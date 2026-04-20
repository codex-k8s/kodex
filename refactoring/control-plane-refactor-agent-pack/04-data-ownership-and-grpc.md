# Правила по данным, БД и gRPC

## 1. Модель хранения данных

Целевое правило - database-per-service.

При этом инфраструктурное упрощение допускается:
- один Postgres server или один кластер;
- у каждого сервиса отдельная database;
- у каждого сервиса отдельный DB user и отдельный secret;
- миграции сервиса применяются только к его базе.

Это не schema-per-service как целевая модель. Это именно отдельные databases в одном инфраструктурном контуре.

## 2. Базовые запреты

Запрещено:
- прямое подключение сервиса к чужой базе;
- любые cross-database join;
- чтение чужих таблиц для "быстрого read-only сценария";
- shared migration folders для нескольких сервисов;
- общий superuser для runtime сервисов.

## 3. Правило source of truth

У каждого типа данных есть один source of truth:
- users и settings - Platform Admin & IAM Service;
- projects, repos, config, tokens - Project Catalog Service;
- runs, flow events, sessions, rate limit waits - Run Orchestrator Service;
- deploy tasks - Runtime Deploy Service;
- interactions - Interaction Service;
- mission control и governance projections - Mission Control Service.

## 4. gRPC как единственный путь межсервисного доступа

Межсервисное взаимодействие строится по двум режимам.

### 4.1. Синхронный режим

Используется, когда нужен:
- command;
- authoritative read;
- access check;
- небольшая по объему служебная выборка.

Это всегда gRPC вызов в сервис-владелец.

### 4.2. Асинхронный режим

Используется, когда нужен:
- fan-out на несколько потребителей;
- построение read models;
- реакция на завершение длительного процесса;
- аналитика, projection, кэш.

Для этого можно использовать outbox и события. Но события не отменяют source of truth. Если нужен authoritative read, сервис идет по gRPC к владельцу.

## 5. Правила для proto и DTO

Нужно соблюдать:
- отдельный proto package на сервис;
- отдельный versioned API namespace;
- не выносить в shared common proto бизнес-сущности разных доменов;
- общие типы держать только для truly-generic вещей: paging, timestamps, health, errors, references.

Нельзя:
- делать единый mega-proto по образцу старого ControlPlaneService;
- передавать через gRPC внутренние SQL-shaped DTO;
- использовать один и тот же message как каноническую модель сразу для нескольких сервисов.

## 6. Данные и миграции

Для каждого сервиса должны существовать:
- свой каталог миграций;
- свой механизм применения миграций;
- своя rollback strategy;
- свой runbook на восстановление;
- своя таблица schema history.

В каждом PR на extraction необходимо явно указать:
- какие таблицы становятся owned этим сервисом;
- какие таблицы удаляются или перестают использоваться;
- какие данные мигрируются;
- нужен ли backfill;
- как проверяется корректность cutover.

## 7. Идемпотентность и отказоустойчивость

Так как межсервисных вызовов станет больше, обязательны:
- idempotency keys для повторяемых команд;
- корреляционные идентификаторы для run-centric операций;
- deadline и retry policy для gRPC клиентов;
- compensating logic вместо распределенных транзакций.

## 8. Минимальные cross-service контракты

Ниже - ориентир для первой волны API.

### Platform Admin & IAM Service
- `ResolvePrincipal`
- `GetUser`
- `CheckProjectAccess`
- `ListProjectMembers`
- `GetSetting`
- `SetSetting`

### Project Catalog Service
- `GetProject`
- `GetRepository`
- `GetRepositoryRuntimeConfig`
- `ListProjectRepositories`
- `GetProjectTokenRef`
- `UpsertWebhookSetup`

### Run Orchestrator Service
- `CreateRunFromWebhook`
- `GetRun`
- `AppendFlowEvent`
- `UpdateRunWaitState`
- `GetResumePayload`
- `UpsertAgentSession`
- `RecordGithubRateLimitWait`

### Runtime Deploy Service
- `PrepareEnvironment`
- `EvaluateRuntimeReuse`
- `GetDeployTask`
- `CancelDeployTask`
- `StopDeployTask`

### Interaction Service
- `CreateInteractionRequest`
- `ClaimDispatch`
- `CompleteDispatch`
- `SubmitCallback`
- `ExpireDueInteractions`
- `GetInteractionResolution`

### Mission Control Service
- `GetWorkspace`
- `GetTimeline`
- `SubmitCommand`
- `ClaimCommand`
- `CompleteCommand`
- `GetGovernanceSnapshot`

## 9. Критерий завершения data-split этапа

Этап data split считается завершенным только если:
- новый сервис имеет свою БД;
- старый код больше не читает старые таблицы по этому сценарию;
- все потребители ходят только в новый gRPC API;
- старые репозитории удалены или выведены из использования;
- документация ownership обновлена.
