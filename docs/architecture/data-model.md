---
id: ARCH-MC-006
title: Логическая модель данных
type: architecture
status: approved
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# Логическая модель данных

## Организации и рабочие области

| Сущность | Ключевые поля |
| --- | --- |
| `Organization` | id, name, slug, status, settings_revision |
| `Membership` | organization_id, subject_id, platform_role |
| `Workspace` | organization_id, name, slug, mattermost_team_id, managed_by |
| `Room` | workspace_id, mattermost_channel_id, room_type, default_agent_id |
| `ConversationBinding` | room_id, root_post_id, session/process reference, lifecycle_state, deletion_requested_at |

## Агенты и инструкции

| Сущность | Ключевые поля |
| --- | --- |
| `RoleDefinition` | organization_id nullable, name, role_type, description, default policies |
| `Agent` | organization_id, role_definition_id, name, bot_identity_id, enabled |
| `AgentAssignment` | agent_id, workspace_id, room_id nullable |
| `InstructionSet` | organization_id, name, source_type, managed_by, current_version_id |
| `InstructionVersion` | instruction_set_id, content manifest, checksum, created_by |
| `RuntimeProfile` | provider type, config template, resource class, image recipe, revision |

## Поставщики и интеграции

| Сущность | Ключевые поля |
| --- | --- |
| `AIProviderAccount` | provider_type, label, credential_ref, auth_status, auth_revision |
| `AccountPool` | selection_policy, allowed account IDs |
| `AccountUsageObservation` | account_id, window, remaining/reset_at, observed_at |
| `IntegrationDefinition` | name, version, schema, capabilities, risk policies |
| `IntegrationConnection` | definition_id, organization_id, config, credential_refs, status |
| `IntegrationGrant` | connection_id, agent_id, capability, constraints |
| `ApprovalRequest` | initiator, capability, safe arguments, state, expires_at |
| `ToolInvocation` | session_id, turn_id, connection_id, arguments_hash, state, approval_id, result_ref |

## Среда выполнения

| Сущность | Ключевые поля |
| --- | --- |
| `AgentSession` | agent_id, provider_account_id, scope, status, archive_ref |
| `Turn` | session_id, source, prompt, status, runtime_revision_id, sequence |
| `AgentDelegation` | source session/turn, target room/thread/session/turn, role, work_item_key, status, callback turn |
| `RuntimeRevision` | effective config manifest, hashes, image digest, created_at |
| `RuntimeLease` | session_id, pod identity, heartbeat, expires_at |
| `UsageObservation` | turn/session/account, limits/tokens/duration |
| `RuntimeResource` | session_id, kind, external_id, state, last_used_at, eligible_at, deleted_at |
| `ResourceRetentionPolicy` | scope, pod_ttl, temporary_ttl, pvc_grace, archive_retention, version |

`provider_account_id` становится неизменяемым после первого запуска сессии. Изменение допустимо только через явное создание новой сессии и передачу контекста.

## Процессы и расписания

| Сущность | Ключевые поля |
| --- | --- |
| `Playbook` | name, coordinator policy, input schema, prompt version, gates |
| `ProcessRun` | playbook version, parent_run_id, state, result, owner_gate |
| `ChildRun` | process_run_id, thread/session target, callback state |
| `AutomationSchedule` | target, cron/interval, timezone, policies, next_run_at |
| `ScheduleOccurrence` | schedule_id, scheduled_for, idempotency_key, status |
| `ScheduledRun` | occurrence_id, process/session reference, outcome |

Уникальный индекс `(schedule_id, scheduled_for)` исключает повторное создание экземпляра расписания.

## Файлы

| Сущность | Ключевые поля |
| --- | --- |
| `Artifact` | organization_id, kind, direction, retention_policy |
| `ArtifactVersion` | artifact_id, storage_key, size, media_type, sha256, scan_status |
| `MessageArtifactBinding` | artifact_version_id, post_id, thread_id, direction |
| `ArtifactDelivery` | artifact_version_id, destination, external_id, state |

## Аудит и исходящий журнал

`AuditEvent` хранит инициатора, действие, цель, исход, `correlation_id` и безопасные метаданные. Необработанные секреты, полное содержимое файлов и неотфильтрованные промпты в аудите не сохраняются.

`OutboxEvent` создается в той же транзакции, что и бизнес-изменение. Обработчик фиксирует ключ идемпотентности и обработанную версию.

## Ключевые инварианты

- Сессия не возобновляется другой учетной записью поставщика.
- Ходы выполняются строго последовательно внутри сессии.
- `RuntimeRevision` неизменяема и относится к одному ходу либо группе идентичных ходов.
- Агент не использует `IntegrationConnection` без действующего права.
- Результат согласования нельзя применить к другому вызову инструмента.
- Ожидающее согласование или внешний обратный вызов блокирует очистку ресурсов среды выполнения.
- PVC удаляется только после подтвержденного архива сессии и наступления `eligible_at`.
- ArtifactVersion immutable; изменение файла создает новую version.
- Git-managed object не изменяется UI до explicit detach.
- Mattermost/Kubernetes external IDs не являются primary business IDs.
