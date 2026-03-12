---
doc_id: DM-S9-MISSION-CONTROL-0001
type: data-model
title: "Mission Control Dashboard — Data model Sprint S9 Day 5"
status: in-review
owner_role: SA
created_at: 2026-03-12
updated_at: 2026-03-12
related_issues: [333, 335, 337, 340, 351, 363]
related_prs: []
approvals:
  required: ["Owner"]
  status: pending
  request_id: "owner-2026-03-12-issue-351-data-model"
---

# Data Model: Mission Control Dashboard

## TL;DR
- Основной owner схемы остаётся `services/internal/control-plane`.
- Ключевые сущности: `mission_control_entities`, `mission_control_relations`, `mission_control_timeline_entries`, `mission_control_commands`, `mission_control_voice_candidates`.
- Основные связи: entity graph + command ledger + timeline projection; `worker` пишет только через owner-controlled use-cases и не становится владельцем схемы.
- Риски миграций: warmup/backfill projections, индексы для active-set queries и rollback после внешних provider side effects.

## Сущности
### Entity: `mission_control_entities`
- Назначение: persisted active-set projection по сущностям `work_item|discussion|pull_request|agent`.
- Важные инварианты:
  - `project_id + entity_kind + entity_external_key` уникальны;
  - `entity_external_key` is the source of truth for transport field `entity_public_id`;
  - internal bigint `id` never leaks into HTTP/gRPC DTO or deep-link contracts;
  - `active_state` принадлежит закрытому enum и используется как primary filter;
  - presentation payload хранится в JSONB, но canonical status/freshness fields остаются typed.

| Field | Type | Nullable | Default | Constraints | Notes |
|---|---|---:|---|---|---|
| id | bigserial | no |  | pk | internal row id |
| project_id | uuid | no |  | fk -> projects | tenant boundary |
| entity_kind | text | no |  | check(work_item/discussion/pull_request/agent) | |
| entity_external_key | text | no |  | unique(project_id, entity_kind, entity_external_key) | provider/platform stable id |
| provider_kind | text | no | `github` | check(github/platform) | MVP baseline |
| provider_url | text | yes |  |  | deep-link target |
| title | text | no |  |  | short card title |
| active_state | text | no |  | check(working/waiting/blocked/review/recent_critical_updates/archived) | default filters |
| sync_status | text | no | `synced` | check(synced/pending_sync/failed/degraded) | |
| projection_version | bigint | no | 1 |  | optimistic concurrency for commands |
| card_payload | jsonb | no | '{}'::jsonb |  | board/list card projection |
| detail_payload | jsonb | no | '{}'::jsonb |  | side panel projection |
| last_timeline_at | timestamptz | yes |  |  | for ordering |
| provider_updated_at | timestamptz | yes |  |  | last seen provider change |
| projected_at | timestamptz | no | now() |  | last projection write |
| stale_after | timestamptz | yes |  |  | freshness threshold |
| created_at | timestamptz | no | now() |  | |
| updated_at | timestamptz | no | now() |  | |

Примечание:
- Все transport references используют `(entity_kind, entity_public_id=entity_external_key)`.
- Поле `id` остаётся только внутренним FK/индексным ключом для схемы `control-plane`.

### Entity: `mission_control_relations`
- Назначение: typed relation graph между сущностями active set.
- Важные инварианты:
  - relation всегда соединяет две существующие `mission_control_entities`;
  - transport exposure relation refs always resolves back to `(entity_kind, entity_public_id)` пары;
  - duplicate relation по `(source_entity_id, relation_kind, target_entity_id)` запрещён.

| Field | Type | Nullable | Default | Constraints | Notes |
|---|---|---:|---|---|---|
| id | bigserial | no |  | pk | |
| project_id | uuid | no |  | fk -> projects | |
| source_entity_id | bigint | no |  | fk -> mission_control_entities | |
| relation_kind | text | no |  | check(linked_to/blocks/blocked_by/formalized_from/owned_by/assigned_to/tracked_by_command) | |
| target_entity_id | bigint | no |  | fk -> mission_control_entities | |
| source_kind | text | no | `platform` | check(platform/provider/command/voice_candidate) | provenance |
| created_at | timestamptz | no | now() |  | |
| updated_at | timestamptz | no | now() |  | |

### Entity: `mission_control_timeline_entries`
- Назначение: unified projection of provider comments/reviews, platform events and command lifecycle.
- Важные инварианты:
  - `entry_external_key` уникален в пределах `(project_id, source_kind)` для dedupe;
  - provider-originated entries read-only in MVP;
  - `entity_id` is an internal FK only; transport returns the owner entity via `entity_public_id`;
  - timeline ordering не зависит от client-side heuristics.

| Field | Type | Nullable | Default | Constraints | Notes |
|---|---|---:|---|---|---|
| id | bigserial | no |  | pk | |
| project_id | uuid | no |  | fk -> projects | |
| entity_id | bigint | no |  | fk -> mission_control_entities | owner entity |
| source_kind | text | no |  | check(provider/platform/command/voice_candidate) | |
| entry_external_key | text | no |  | unique(project_id, source_kind, entry_external_key) | dedupe key |
| command_id | uuid | yes |  | fk -> mission_control_commands | nullable |
| summary | text | no |  |  | short UI summary |
| body_markdown | text | yes |  |  | provider or platform body |
| payload | jsonb | no | '{}'::jsonb |  | typed extras by source kind |
| occurred_at | timestamptz | no |  | index | canonical ordering |
| provider_url | text | yes |  |  | deep-link for provider entries |
| is_read_only | bool | no | true |  | provider entries stay true in MVP |
| created_at | timestamptz | no | now() |  | |

### Entity: `mission_control_commands`
- Назначение: command ledger для inline write-path и reconciliation evidence.
- Важные инварианты:
  - `business_intent_key` уникален на проект в пределах активного semantic window;
  - commands that require explicit approval must enter `pending_approval` before any provider mutation or label transition;
  - command state machine monotonic;
  - every mutating command writes audit and status transition.

| Field | Type | Nullable | Default | Constraints | Notes |
|---|---|---:|---|---|---|
| id | uuid | no | gen_random_uuid() | pk | public command id |
| project_id | uuid | no |  | fk -> projects | |
| command_kind | text | no |  | check(discussion.create/work_item.create/discussion.formalize/stage.next_step.execute/command.retry_sync) | |
| target_entity_id | bigint | yes |  | fk -> mission_control_entities | optional for create commands |
| actor_id | text | no |  |  | JWT principal / platform actor |
| business_intent_key | text | no |  | unique(project_id, business_intent_key) | dedupe anchor |
| correlation_id | text | no |  | unique | end-to-end audit correlation |
| status | text | no | `accepted` | check(accepted/pending_approval/queued/pending_sync/reconciled/failed/blocked/cancelled) | |
| failure_reason | text | yes |  | check(provider_error/policy_denied/projection_stale/duplicate_intent/timeout/approval_denied/approval_expired/unknown) | |
| approval_request_id | uuid | yes |  |  | nullable until approval flow required |
| approval_state | text | no | `not_required` | check(not_required/pending/approved/denied/expired) | |
| approval_requested_at | timestamptz | yes |  |  | |
| approval_decided_at | timestamptz | yes |  |  | |
| payload | jsonb | no | '{}'::jsonb |  | typed request body |
| result_payload | jsonb | no | '{}'::jsonb |  | typed status/result + approval metadata |
| provider_delivery_ids | jsonb | no | '[]'::jsonb |  | observed provider events |
| requested_at | timestamptz | no | now() |  | |
| updated_at | timestamptz | no | now() |  | |
| reconciled_at | timestamptz | yes |  |  | terminal success |

### Entity: `mission_control_voice_candidates`
- Назначение: isolated draft contour for optional voice-first intake.
- Важные инварианты:
  - voice candidate never becomes active-set entity without explicit promotion;
  - promotion creates relation to resulting discussion/task or command.

| Field | Type | Nullable | Default | Constraints | Notes |
|---|---|---:|---|---|---|
| id | uuid | no | gen_random_uuid() | pk | |
| project_id | uuid | no |  | fk -> projects | |
| source_kind | text | no |  | check(voice/audio_upload/transcript) | |
| status | text | no | `draft` | check(draft/promoted/rejected/expired) | |
| transcript_excerpt | text | yes |  |  | |
| structured_summary | text | yes |  |  | |
| payload | jsonb | no | '{}'::jsonb |  | raw transcript/meta, policy-safe only |
| promoted_entity_id | bigint | yes |  | fk -> mission_control_entities | nullable |
| promoted_command_id | uuid | yes |  | fk -> mission_control_commands | nullable |
| created_by | text | no |  |  | |
| created_at | timestamptz | no | now() |  | |
| updated_at | timestamptz | no | now() |  | |

## JSONB payload variant mapping
### `mission_control_entities.detail_payload`
| `entity_kind` | Stored variant |
|---|---|
| `work_item` | `WorkItemDetailsPayload` |
| `discussion` | `DiscussionDetailsPayload` |
| `pull_request` | `PullRequestDetailsPayload` |
| `agent` | `AgentDetailsPayload` |

### `mission_control_timeline_entries.payload`
| `source_kind` | Stored variant |
|---|---|
| `provider` | `ProviderTimelinePayload` |
| `platform` | `PlatformTimelinePayload` |
| `command` | `CommandTimelinePayload` |
| `voice_candidate` | `VoiceCandidateTimelinePayload` |

### `mission_control_commands.payload`
| `command_kind` | Stored variant |
|---|---|
| `discussion.create` | `DiscussionCreatePayload` |
| `work_item.create` | `WorkItemCreatePayload` |
| `discussion.formalize` | `DiscussionFormalizePayload` |
| `stage.next_step.execute` | `StageNextStepExecutePayload` |
| `command.retry_sync` | `RetrySyncPayload` |

### `mission_control_commands.result_payload`
| Status/event scope | Stored variant |
|---|---|
| acknowledgement | `MissionControlCommandAck` |
| approval metadata | `MissionControlApprovalStatus` |
| reconcile result | `MissionControlCommandResultPayload` |

## Связи
- `projects` 1:N `mission_control_entities`
- `mission_control_entities` 1:N `mission_control_timeline_entries`
- `mission_control_entities` N:N `mission_control_entities` via `mission_control_relations`
- `mission_control_entities` 1:N `mission_control_commands`
- `mission_control_commands` 1:N `mission_control_timeline_entries`
- `mission_control_voice_candidates` 0..1 -> `mission_control_commands`
- `mission_control_voice_candidates` 0..1 -> `mission_control_entities`

## Индексы и запросы (критичные)
- Query: dashboard active set by filter + search + view_mode
  - Indexes:
    - `(project_id, active_state, updated_at desc)`
    - `(project_id, sync_status, updated_at desc)`
    - GIN on `card_payload` for limited search facets
- Query: entity details by kind/id
  - Index:
    - unique `(project_id, entity_kind, entity_external_key)`
- Query: relations for selected entity
  - Indexes:
    - `(source_entity_id, relation_kind)`
    - `(target_entity_id, relation_kind)`
- Query: timeline by entity ordered desc
  - Index:
    - `(entity_id, occurred_at desc, id desc)`
- Query: command lookup and dedupe
  - Indexes:
    - unique `(project_id, business_intent_key)`
    - `(project_id, status, updated_at desc)`
    - unique `(correlation_id)`
- Query: voice candidate moderation
  - Index:
    - `(project_id, status, updated_at desc)`

## Политика хранения данных
- `mission_control_entities`:
  - active rows keep current projection; archived state retained for traceability until broader retention policy defined.
- `mission_control_relations`:
  - hard delete not expected in MVP; obsolete relations marked by update/replace from owner use-case.
- `mission_control_timeline_entries`:
  - append-only except dedupe-safe no-op on duplicate provider delivery.
- `mission_control_commands`:
  - terminal rows retained for audit and reconciliation evidence.
- `mission_control_voice_candidates`:
  - remain isolated from core dashboard unless promoted; rejected/expired retained for policy audit.

## Доменные инварианты
- `projection_version` increments on every entity mutation visible to commands.
- `entity_external_key` remains stable across warmup/rebuild and is the only public identifier exposed by transport.
- `business_intent_key` represents semantic uniqueness, not transport retry uniqueness.
- Duplicate provider delivery must not create new command row or duplicate timeline entry.
- `degraded` entity/snapshot state does not alter canonical provider/platform truth; it only marks freshness and UX safety envelope.
- Voice candidate cannot bypass discussion/task creation guardrails.
- `stage.next_step.execute` cannot move from `pending_approval` to `queued` without recorded `approval_request_id` and `approval_state=approved`.

## Ownership and write-path
- `control-plane`:
  - owns schema, repositories, projection updates, command status transitions.
- `worker`:
  - writes through owner use-cases for provider sync/reconcile, never directly shaping final schema semantics.
- `api-gateway` and `web-console`:
  - never persist canonical Mission Control state.

## Runtime impact / Migration impact
- Runtime impact (`run:design`): absent, docs only.
- Migration impact (`run:dev`):
  - additive creation of new tables and indexes;
  - warmup/backfill from existing issue/PR/run/provider state before enabling write-path;
  - no destructive rewrite of existing platform tables required.
