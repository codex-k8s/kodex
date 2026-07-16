---
id: ARCH-MC-003
title: Карта доменов
type: architecture
status: proposed
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# Карта доменов

| Домен | Владеет | Не владеет |
| --- | --- | --- |
| Identity & Access | Organization, Membership, PlatformRole, Policy | Mattermost users, provider credentials |
| Workspaces & Conversations | Workspace, Room, ThreadBinding, ConversationBinding | Mattermost posts/files как blobs |
| Agents & Instructions | RoleDefinition, Agent, AgentAssignment, InstructionSet | Session execution, external credentials |
| Providers & Accounts | ProviderDefinition, AIProviderAccount, AccountPool, usage observations | Agent prompts, sessions другого account |
| Runtime Orchestration | AgentSession, Turn, RuntimeRevision, RuntimeLease | Kubernetes как бизнес source of truth |
| Processes & Automations | Playbook, ProcessRun, ChildRun, AutomationSchedule, ScheduledRun | Выполнение integration mutation |
| Integrations & Approvals | IntegrationDefinition, Connection, Capability, Grant, ApprovalRequest | Agent lifecycle, UI state |
| Artifacts & Knowledge | Artifact, ArtifactVersion, Delivery, KnowledgeSpace | Mattermost file metadata как source of truth |
| Images & Supply Chain | RoleImageRecipe, ImageBuild, ImageArtifact | Runtime session state |
| Audit & Observability | AuditEvent, correlation model, operational read models | Доменные решения других contexts |

## Разрешенные направления зависимостей

```text
Identity
  -> Workspaces
  -> Agents

Agents
  -> Providers
  -> Integrations
  -> Images

Conversations / Automations / Processes
  -> Runtime Orchestration

Runtime Orchestration
  -> Providers
  -> Integrations
  -> Artifacts
  -> Kubernetes adapter

Все домены -> Audit / Outbox
```

## Правила границ

- Домен не читает таблицы другого домена напрямую.
- Ссылка на чужую сущность хранит stable ID и проверяется через port/use case.
- Cross-domain изменения выполняются командой/event, а не общей SQL transaction, кроме этапа modular monolith с явно оформленным application transaction coordinator.
- Транспортные DTO не становятся доменными моделями.
- Provider-specific поля хранятся в typed adapter config и не просачиваются в универсальные сущности.
- `Project`, `Chat` и текущая `AgentRole` являются migration aliases для Workspace, Room и Agent/RoleDefinition.

## Миграция текущей модели

| Текущая сущность | Целевая сущность | Правило миграции |
| --- | --- | --- |
| `projects` | `workspaces` | Один к одному; Mattermost team binding сохраняется. |
| `chats` | `rooms` | Один к одному; channel binding сохраняется. |
| `agent_roles` | `role_definitions` + `agents` | Для каждой project role создается definition/agent без изменения bot identity. |
| `openai_accounts` | `ai_provider_accounts` | Provider type `openai-codex`, secret reference сохраняется. |
| GitHub accounts/repos/env | Integration connections/grants | Миграция после появления GitHub integration package. |
| prompt templates/config overlay | InstructionSet/RuntimeProfile | Сначала compatibility adapter, затем ownership migration. |
| agent sessions/turns | Runtime domain | IDs и session archive сохраняются. |
