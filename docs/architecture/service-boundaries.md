---
id: ARCH-MC-004
title: Границы сервисов и структура репозитория
type: architecture
status: proposed
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# Границы сервисов и структура репозитория

## Целевая структура

```text
apps/
  control-center/
services/
  external/
    interaction-gateway/
  internal/
    control-plane/
    runtime-controller/
    integration-gateway/
  jobs/
    automation-scheduler/
    agent-runner/
  dev/
libs/go/
proto/
specs/
  openapi/
  asyncapi/
config/catalog/
  roles/
  integrations/
  playbooks/
deploy/
  helm/
  gitops/
docs/
```

Каждый Go service использует локальные `cmd`, `internal/app`, `internal/domain`, `internal/repository`, `internal/clients` и `internal/transport`. `libs/go/**` содержит только observability, auth context, typed IDs, clock и другие действительно cross-cutting primitives с несколькими реальными consumers. Детальный layout определен в `docs/design-guidelines/go/services_design_requirements.md`.

## Границы deployables

### control-plane

- CRUD и validation business entities;
- effective configuration и policy evaluation;
- OpenAPI для Control Center;
- transactional outbox;
- migrations своих schemas.

Не выполняет Kubernetes reconciliation, AI runtime и внешние mutations.

### interaction-gateway

- Mattermost WebSocket/REST;
- cards, dialogs, reactions, bot identities;
- inbound files и outbound deliveries;
- mapping post/thread to platform commands;
- idempotency входных событий.

Не владеет sessions, schedules и integrations.

### runtime-controller

- reconcile session pods/PVCs/secrets/config resources;
- RuntimeRevision application;
- capacity/admission/idle eviction/TTL;
- lifecycle и status Kubernetes resources.

### integration-gateway

- MCP transport;
- connection resolution;
- grants, risk, approvals;
- credential isolation;
- idempotent tool execution.

### automation-scheduler

- due schedule claim;
- occurrence idempotency;
- misfire/concurrency policy;
- enqueue ScheduledRun;
- next-run calculation.

### agent-runner

- turn claim/complete;
- AI runtime process lifecycle;
- session restore/snapshot;
- workspace materialization;
- local artifact publish bridge.

## Переход от текущего bot-service

1. Зафиксировать characterization tests существующих flows.
2. Ввести bounded-context packages и отдельные repository interfaces внутри текущего binary.
3. Разделить общий `admin.Repository` по владельцам данных.
4. Вынести Mattermost transport из domain services.
5. Ввести outbox и idempotent command handlers.
6. Выделить runtime-controller первым самостоятельным service.
7. Выделить integration-gateway после появления integration model.
8. Выделить interaction-gateway и control-plane после стабилизации OpenAPI.
9. Удалить compatibility facade только после миграции UI и live data.

Нельзя одновременно менять модель данных, transport, service split и user behavior без промежуточного совместимого состояния.

## Внутренние контракты

- External/control API: OpenAPI.
- Mattermost callbacks: typed HTTP models на основе upstream SDK.
- Domain events/commands: AsyncAPI и versioned envelopes.
- Internal streaming/high-throughput: Protobuf/gRPC только после измеренной необходимости.
- MCP: официальный Model Context Protocol Go SDK.
