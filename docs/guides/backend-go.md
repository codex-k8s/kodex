---
id: GUIDE-MC-003
title: Backend на Go
type: guide
status: proposed
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# Backend на Go

## Архитектура

- Entrypoint и transport остаются тонкими.
- Use case имеет один явный входной command/query и typed result.
- Domain rules не зависят от SDK external systems.
- PostgreSQL repositories разделены по bounded context.
- Long-running loops моделируются controller/scheduler/worker, а не goroutine из HTTP composition root без leadership.
- External side effects используют idempotency keys и retries с классификацией ошибок.

## Ошибки

- Domain errors typed и не содержат secret/raw payload.
- Transport переводит errors в стабильный contract.
- Retryability определяется typed category, а не поиском произвольной строки, кроме изолированного provider adapter с тестами.
- `context.Canceled` и deadline обрабатываются отдельно от business failure.

## Concurrency

- Turn/session invariants обеспечиваются database state/locks, а не только in-memory mutex.
- Workers используют bounded concurrency.
- Background tasks имеют graceful shutdown, lease/heartbeat и recovery.
- Clock инъецируется в schedule/TTL tests.

## External systems

- Используется официальный SDK/library, если он поддерживается и не ломает contract.
- HTTP client имеет timeout, connection limits, telemetry и safe error body limit.
- Kubernetes reconciliation строится на `controller-runtime`/client-go typed APIs.
- Mattermost использует upstream models/client.
- MCP использует официальный Go SDK.

## Data

- Migrations выполняются goose и принадлежат service/domain.
- SQL queries typed; JSONB используется для versioned flexible config, а не для сокрытия неизвестной схемы.
- Cross-domain SQL запрещен как бизнес-контракт.
- Transactional outbox создается в business transaction.

## Тесты

- Domain unit tests;
- application tests с fakes/real PostgreSQL по риску;
- repository integration tests;
- contract tests adapters;
- race-sensitive tests с `-race` на релевантных packages;
- characterization tests до refactoring legacy behavior;
- E2E для Mattermost/runtime/integration gates.

Подробные правила: `docs/design-guidelines/go/**`.
