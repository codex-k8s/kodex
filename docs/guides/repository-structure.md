---
id: GUIDE-MC-002
title: Структура монорепозитория
type: guide
status: proposed
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# Структура монорепозитория

## Верхний уровень

```text
apps/                 # Пользовательские приложения
services/             # Deployables по зонам external/internal/jobs/dev
libs/go/              # Минимальные shared Go primitives
proto/                # Protobuf/gRPC source и generated contracts
specs/                # OpenAPI/AsyncAPI source contracts
config/catalog/       # Versioned role/integration/playbook seeds
deploy/               # Helm и GitOps desired state
docs/                 # Product/architecture/domain/guides/operations/ADR
scripts/              # Короткие developer/bootstrap wrappers
tools/                # Repo tooling и generators
```

## Go service

```text
services/<zone>/<service>/
  cmd/<service>/main.go
  internal/
    app/
    domain/
    repository/
    clients/
    transport/
  cmd/cli/migrations/
  Dockerfile
```

- `cmd` загружает config/logger/signals и вызывает composition root.
- `domain` не импортирует transport/Kubernetes/Mattermost/PostgreSQL.
- `domain/service` реализует use cases и transaction boundaries.
- `domain/repository` определяет необходимые interfaces.
- `repository` и `clients` реализуют PostgreSQL, Kubernetes, Mattermost, S3 и provider adapters.
- `transport` преобразует external DTO в application commands.

## Shared code

Shared package создается только при наличии минимум двух реальных consumers и стабильного контракта. Domain types не выносятся в общий `models` пакет.

Допустимые shared primitives:

- typed IDs/correlation;
- clock/UUID abstractions;
- observability bootstrap;
- safe logging/redaction;
- auth context;
- generated contracts.

## Contracts

Source contract редактируется в `specs/**` или `proto/**`; generated code не редактируется вручную. Каждая генерация воспроизводима одной repo-командой и проверяется CI на clean diff.

## Миграция текущей структуры

До выделения сервисов текущий bot-service остается рабочим compatibility deployable. Новая логика появляется в целевых модулях, а старые handlers делегируют им. Массовое перемещение без behavior tests запрещено.
