---
id: ARCH-MC-002
title: Высокоуровневая архитектура
type: architecture
status: proposed
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# Высокоуровневая архитектура

```mermaid
flowchart LR
    U[Пользователь] --> MM[Mattermost]
    U --> CC[Control Center]
    MM --> IG[Interaction Gateway]
    CC --> CP[Control Plane]
    IG --> CP
    CP --> PG[(PostgreSQL)]
    CP --> OB[(Transactional Outbox)]
    AS[Automation Scheduler] --> PG
    AS --> OB
    OB --> RC[Runtime Controller]
    RC --> K8S[Kubernetes API]
    K8S --> AR[Agent Runner Pod]
    AR --> AI[AI Runtime Provider]
    AR --> MG[Integration Gateway MCP]
    MG --> EXT[External Systems]
    MG --> AP[Human Approval]
    AR --> S3[(S3 Artifact Store)]
    IG --> S3
    IG --> MM
    CP --> OT[OpenTelemetry]
    IG --> OT
    RC --> OT
    MG --> OT
```

## Control plane

Хранит desired state и бизнес-модель: organizations, workspaces, agents, providers, integrations, instructions, playbooks, schedules, sessions, artifacts metadata, approvals и audit.

Control plane не создает Kubernetes pod напрямую из HTTP handler. Он фиксирует транзакционное изменение и публикует durable command через outbox.

## Interaction gateway

Обрабатывает Mattermost events, slash fallback, interactive cards, dialogs, bot identities, reactions, file delivery и thread updates.

Gateway не владеет agent/session бизнес-состоянием. Повторно доставленный Mattermost event должен быть безопасен по `event_id/post_id`.

## Runtime controller

Сопоставляет desired runtime state с Kubernetes resources. Reconcile идемпотентен и использует детерминированные имена, labels, owner references и status conditions.

Controller решает:

- какой session pod должен существовать;
- какой RuntimeRevision должен быть применен;
- достаточно ли capacity;
- какой idle pod можно освободить;
- когда session pod завершить по TTL;
- когда восстановить queued turn после transient failure.

## Agent runner

Runner является process supervisor внутри session pod:

- получает и подтверждает turn;
- материализует config, auth, instructions и attachments;
- запускает AI runtime adapter;
- стримит progress и usage;
- вызывает разрешенные MCP tools;
- публикует final result;
- сохраняет session archive;
- корректно reap-ит дочерние процессы и обрабатывает termination.

Runner не содержит Mattermost, project onboarding и approval business logic.

## Integration gateway

Предоставляет session-scoped MCP endpoint. Он аутентифицирует agent session, вычисляет grants, маскирует данные, создает approvals и выполняет внешние действия от имени IntegrationConnection.

Опасный credential остается в gateway/secret backend и не передается agent pod.

## Automation scheduler

Выбирает due AutomationSchedules, создает уникальные occurrences и ставит ScheduledRuns в общую очередь. Scheduler не запускает pod напрямую и не использует Kubernetes CronJob как бизнес-модель.

## Consistency model

- Внутри bounded context — PostgreSQL transaction.
- Между contexts — transactional outbox и идемпотентные consumers.
- Kubernetes/Mattermost/external APIs — eventual consistency с status и retry.
- Human approval — durable wait state, а не удержание HTTP request.
