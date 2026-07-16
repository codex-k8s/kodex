---
id: ARCH-MC-001
title: Архитектурный baseline MatterCodex
type: architecture-index
status: proposed
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# Архитектурный baseline MatterCodex

Архитектура MatterCodex строится как provider-neutral control plane для ИИ-сотрудников с Mattermost interaction surface и Kubernetes runtime.

## Основные принципы

- Эволюция действующего инстанса без big-bang rewrite.
- Сначала строгие модули и контракты, затем физическое выделение deployables.
- PostgreSQL является источником истины для metadata, desired state, очередей и audit.
- S3-compatible storage является источником истины для artifacts и session archives.
- Kubernetes является runtime substrate, а не источником бизнес-состояния.
- Mattermost хранит разговорное представление, но не заменяет ProcessRun, Turn и AuditEvent.
- AI runtime, GitHub, Kubernetes и внешние бизнес-системы подключаются через provider/integration contracts.
- Любая внешняя mutation имеет idempotency, capability grant, risk policy и audit.
- Код не полагается на shell как на orchestration layer.

## Документы раздела

| Код | Файл | Назначение |
| --- | --- | --- |
| `ARCH-MC-001` | `docs/architecture/README.md` | Индекс и принципы. |
| `ARCH-MC-002` | `docs/architecture/high-level-architecture.md` | Компоненты и потоки. |
| `ARCH-MC-003` | `docs/architecture/domain-map.md` | Bounded contexts и зависимости. |
| `ARCH-MC-004` | `docs/architecture/service-boundaries.md` | Границы deployables и переход. |
| `ARCH-MC-005` | `docs/architecture/integration-map.md` | Внешние системы и integration modes. |
| `ARCH-MC-006` | `docs/architecture/data-model.md` | Сущности, владение данными и инварианты. |
| `ARCH-MC-007` | `docs/architecture/runtime-and-sessions.md` | Session/turn runtime и account affinity. |
| `ARCH-MC-008` | `docs/architecture/attachments-and-artifacts.md` | Входные и выходные файлы. |
| `ARCH-MC-009` | `docs/architecture/automations-and-playbooks.md` | Schedules, processes и callbacks. |

## Технологический baseline

- Go для backend, controllers, gateways и runner.
- Vue 3 + TypeScript для Control Center.
- PostgreSQL для transactional state.
- S3-compatible object storage для blobs.
- Kubernetes для platform и agent workloads.
- Mattermost REST/WebSocket APIs и официальный Go model/client.
- OpenAPI для внешних HTTP contracts.
- AsyncAPI для durable events.
- Protobuf/gRPC только для оправданных внутренних high-throughput contracts.
- OpenTelemetry, Prometheus и Grafana для observability.
- BuildKit для role image builds.

Конкретные версии зависимостей фиксируются в dependency catalog и lock files, а не в архитектурных документах.
