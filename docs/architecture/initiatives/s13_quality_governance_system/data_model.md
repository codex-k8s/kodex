---
doc_id: DM-S13-QG-0001
type: data-model
title: "Quality Governance System — Data model Sprint S13 Day 5"
status: in-review
owner_role: SA
created_at: 2026-03-16
updated_at: 2026-03-16
related_issues: [466, 469, 470, 471, 476, 484, 488, 494, 512]
related_prs: []
approvals:
  required: ["Owner"]
  status: pending
  request_id: "owner-2026-03-16-issue-494-data-model"
---

# Data Model: Quality Governance System

## TL;DR
- Schema owner остаётся `services/internal/control-plane`.
- Canonical aggregate состоит из package root, hidden draft ledger, semantic wave lineage, evidence blocks, decision records, feedback records, projection snapshots и artifact links.
- Главный миграционный риск: не смешать hidden draft history, publishable waves и explicit decisions в один плоский статус.

## Сущности
### Entity: `change_governance_packages`
- Назначение: aggregate root для canonical change-governance state.
- Важные инварианты:
  - один package root на `package_key`;
  - `risk_tier`, `evidence_completeness_state`, `verification_minimum_state`, `waiver_state`, `release_readiness_state`, `governance_feedback_state` остаются отдельными полями;
  - `publication_state=review_ready` невозможен без минимум одной published wave;
  - `release_readiness_state=ready|conditionally_ready|released` невозможен при `risk_tier in (high, critical)` и implicit waiver.

| Field | Type | Nullable | Default | Constraints | Notes |
|---|---|---:|---|---|---|
| id | uuid | no | gen_random_uuid() | pk | aggregate id |
| package_key | text | no |  | unique | immutable domain key |
| project_id | uuid | no |  | fk -> projects | tenant boundary |
| repository_full_name | text | no |  | index | repo namespace |
| issue_number | int | no |  | index | primary issue lineage |
| pr_number | int | yes |  | index | current PR lineage |
| risk_tier | text | yes |  | check(low/medium/high/critical) | explicit classification |
| bundle_admissibility | text | no | `requires_decomposition` | check(single_wave/mechanical_bounded_scope/requires_decomposition) | publication discipline |
| publication_state | text | no | `hidden_draft` | check(hidden_draft/wave_map_defined/waves_published/review_ready/release_decided/feedback_open/closed) | package lifecycle |
| evidence_completeness_state | text | no | `not_started` | check(not_started/partial/complete/gapped/waived) | separate construct |
| verification_minimum_state | text | no | `not_started` | check(not_started/in_progress/met/failed/waived) | separate construct |
| waiver_state | text | no | `none` | check(none/requested/approved/rejected/expired) | active waiver summary |
| release_readiness_state | text | no | `not_ready` | check(not_ready/conditionally_ready/ready/blocked/released) | release gate summary |
| governance_feedback_state | text | no | `none` | check(none/open/reclassified/closed) | late gap loop |
| active_projection_version | bigint | no | 1 |  | CAS/read freshness |
| latest_correlation_id | text | yes |  | index | latest audit correlation |
| created_at | timestamptz | no | now() |  | |
| updated_at | timestamptz | no | now() |  | |

### Entity: `change_governance_internal_drafts`
- Назначение: internal-only ledger для hidden `internal working draft`.
- Важные инварианты:
  - draft metadata never becomes source for user-facing projections;
  - raw prompt/diff content is not persisted here;
  - multiple internal drafts may exist, but only latest active draft per package is marked `is_latest=true`.

| Field | Type | Nullable | Default | Constraints | Notes |
|---|---|---:|---|---|---|
| id | uuid | no | gen_random_uuid() | pk | draft ledger id |
| package_id | uuid | no |  | fk -> change_governance_packages | owning package |
| run_id | uuid | yes |  | fk -> agent_runs | originating run |
| signal_id | text | no |  | unique | idempotency anchor |
| draft_ref | text | no |  |  | runner-local hidden reference |
| draft_checksum | text | yes |  |  | dedupe hash |
| draft_kind | text | no | `internal_working_draft` | check(internal_working_draft) | closed enum |
| metadata_json | jsonb | no | '{}'::jsonb |  | typed hints only, no raw draft text |
| is_latest | bool | no | true |  | partial unique on latest draft per package |
| occurred_at | timestamptz | no | now() |  | |
| created_at | timestamptz | no | now() |  | |

### Entity: `change_governance_waves`
- Назначение: semantic wave lineage для первой publishable unit.
- Важные инварианты:
  - `publish_order` уникален внутри package;
  - wave не может стать `published` без package root;
  - `mechanical_bounded_scope` допустим только при `dominant_intent=mechanical_refactor|docs_only`.

| Field | Type | Nullable | Default | Constraints | Notes |
|---|---|---:|---|---|---|
| id | uuid | no | gen_random_uuid() | pk | wave id |
| package_id | uuid | no |  | fk -> change_governance_packages | owning package |
| wave_key | text | no |  | unique(package_id, wave_key) | stable human-readable key |
| publish_order | int | no |  | unique(package_id, publish_order) | sequence |
| dominant_intent | text | no |  | check(code_behavior/schema/transport/ui/ops/mechanical_refactor/docs_only) | semantic class |
| bounded_scope_kind | text | no |  | check(single_context/cross_context/mechanical_bounded_scope) | boundedness |
| publication_state | text | no | `planned` | check(planned/published/reviewed/merged/superseded) | lifecycle |
| evidence_completeness_state | text | no | `not_started` | check(not_started/partial/complete/gapped/waived) | wave summary |
| verification_minimum_state | text | no | `not_started` | check(not_started/in_progress/met/failed/waived) | wave summary |
| summary | text | no |  |  | user-safe wave summary |
| created_at | timestamptz | no | now() |  | |
| updated_at | timestamptz | no | now() |  | |

### Entity: `change_governance_evidence_blocks`
- Назначение: typed completeness and verification blocks for package or wave.
- Важные инварианты:
  - block is scoped either to package or to wave;
  - `required_by_tier=true` blocks must remain visible until `state in (present, verified, waived)`;
  - verification state does not auto-close waiver state.

| Field | Type | Nullable | Default | Constraints | Notes |
|---|---|---:|---|---|---|
| id | uuid | no | gen_random_uuid() | pk | block id |
| package_id | uuid | no |  | fk -> change_governance_packages | owning package |
| wave_id | uuid | yes |  | fk -> change_governance_waves | null for package-scoped block |
| block_kind | text | no |  | check(intent_contract/verification/review_waiver/release_readiness/runtime_feedback) | evidence family |
| state | text | no | `missing` | check(missing/present/verified/waived/stale) | completeness state |
| verification_state | text | no | `not_started` | check(not_started/in_progress/met/failed/waived) | verification construct |
| required_by_tier | bool | no | false |  | mandatory flag |
| source_kind | text | no |  | check(agent_signal/github_webhook/staff_command/worker_feedback/backfill_inferred) | provenance |
| artifact_links_json | jsonb | no | '[]'::jsonb |  | typed artifact refs |
| latest_signal_id | text | yes |  |  | idempotent update anchor |
| observed_at | timestamptz | no | now() |  | |
| created_at | timestamptz | no | now() |  | |
| updated_at | timestamptz | no | now() |  | |

### Entity: `change_governance_decision_records`
- Назначение: append-only ledger для classification, reclassification, waiver и release-readiness decisions.
- Важные инварианты:
  - decisions append-only; supersession happens through new rows;
  - `approved` waiver for `high/critical` requires non-null `residual_risk_tier`;
  - only one active decision per `(package_id, decision_kind, scope_kind, scope_ref)` remains current in projections.

| Field | Type | Nullable | Default | Constraints | Notes |
|---|---|---:|---|---|---|
| id | uuid | no | gen_random_uuid() | pk | decision row |
| package_id | uuid | no |  | fk -> change_governance_packages | owning package |
| scope_kind | text | no | `package` | check(package/wave/evidence_block) | decision scope |
| scope_ref | text | no |  |  | package id, wave key or block id |
| decision_id | text | no |  | unique | idempotency anchor |
| decision_kind | text | no |  | check(risk_classification/reclassification/waiver/release_readiness) | decision class |
| state | text | no |  | check(proposed/approved/rejected/superseded) | lifecycle |
| actor_kind | text | no |  | check(owner/reviewer/operator/system) | who decided |
| residual_risk_tier | text | yes |  | check(low/medium/high/critical) | waiver/release context |
| summary_markdown | text | no |  |  | user-safe rationale |
| decision_payload_json | jsonb | no | '{}'::jsonb |  | typed closed-union payload |
| recorded_at | timestamptz | no | now() |  | |
| created_at | timestamptz | no | now() |  | |

### Entity: `change_governance_feedback_records`
- Назначение: explicit governance-gap ledger from review/release/postdeploy/remediation.
- Важные инварианты:
  - feedback records can reopen package states but never rewrite historical decisions;
  - `late_reclassification` feedback must create or supersede a `reclassification` decision record;
  - backfill-created gaps remain marked `source_kind=backfill`.

| Field | Type | Nullable | Default | Constraints | Notes |
|---|---|---:|---|---|---|
| id | uuid | no | gen_random_uuid() | pk | feedback id |
| package_id | uuid | no |  | fk -> change_governance_packages | owning package |
| feedback_id | text | no |  | unique | idempotency anchor |
| gap_kind | text | no |  | check(under_classified/missing_evidence/verification_bypass/silent_waiver_attempt/semantic_mix/late_reclassification) | gap type |
| source_kind | text | no |  | check(review/release/postdeploy/remediation/worker_sweep/backfill) | origin |
| severity | text | no |  | check(medium/high/critical) | queue priority |
| state | text | no | `open` | check(open/acknowledged/reclassified/closed) | lifecycle |
| suggested_action | text | no |  | check(reclassify/request_evidence/record_waiver/block_release/close_gap) | typed CTA |
| summary_markdown | text | no |  |  | user-safe summary |
| related_artifact_ref | text | yes |  |  | optional issue/pr/run/doc ref |
| opened_at | timestamptz | no | now() |  | |
| closed_at | timestamptz | yes |  |  | |
| created_at | timestamptz | no | now() |  | |
| updated_at | timestamptz | no | now() |  | |

### Entity: `change_governance_projection_snapshots`
- Назначение: persisted projections for queue/detail/operator/comment surfaces.
- Важные инварианты:
  - one current snapshot per `(package_id, projection_kind)`;
  - snapshots are derived data and can be rebuilt;
  - comment mirror projection excludes hidden draft metadata by design.

| Field | Type | Nullable | Default | Constraints | Notes |
|---|---|---:|---|---|---|
| id | bigserial | no |  | pk | |
| package_id | uuid | no |  | fk -> change_governance_packages | owner package |
| projection_kind | text | no |  | check(package_list/package_detail/operator_gap_queue/release_gate/github_status_comment) | closed enum |
| projection_version | bigint | no |  | unique(package_id, projection_kind, projection_version) | monotonic |
| is_current | bool | no | true |  | partial unique current snapshot |
| payload_json | jsonb | no | '{}'::jsonb |  | typed projection payload |
| refreshed_at | timestamptz | no | now() |  | |
| created_at | timestamptz | no | now() |  | |

### Entity: `change_governance_artifact_links`
- Назначение: auditable lineage between package and issue/PR/run/doc/comment artifacts.
- Важные инварианты:
  - primary issue link is unique per package;
  - one artifact may participate in several relation kinds, but duplicate exact tuples are forbidden.

| Field | Type | Nullable | Default | Constraints | Notes |
|---|---|---:|---|---|---|
| id | bigserial | no |  | pk | |
| package_id | uuid | no |  | fk -> change_governance_packages | owner package |
| artifact_kind | text | no |  | check(issue/pull_request/run/agent_session/document/service_comment/release_note) | linked artifact |
| artifact_ref | text | no |  |  | stable ref |
| relation_kind | text | no |  | check(primary_context/evidence_source/decision_followup/feedback_source) | relation semantics |
| display_label | text | no |  |  | user-safe label |
| created_at | timestamptz | no | now() |  | |

### Entity: `flow_events` (extension)
- Назначение: audit mirror for governance lifecycle.
- Day5 decision:
  - table stays source-of-truth for global audit;
  - nullable field `change_governance_package_id` may be added for direct joins;
  - payload remains sanitized and references package ids, wave keys and decision ids instead of raw draft content.

## Связи
- `change_governance_packages 1:N change_governance_internal_drafts`
- `change_governance_packages 1:N change_governance_waves`
- `change_governance_packages 1:N change_governance_evidence_blocks`
- `change_governance_packages 1:N change_governance_decision_records`
- `change_governance_packages 1:N change_governance_feedback_records`
- `change_governance_packages 1:N change_governance_projection_snapshots`
- `change_governance_packages 1:N change_governance_artifact_links`
- `change_governance_waves 1:N change_governance_evidence_blocks`

## Индексы и запросы (критичные)
- Package queue:
  - `(project_id, risk_tier, publication_state, updated_at desc)`
- Owner/reviewer ready queue:
  - partial index on packages where `publication_state in ('waves_published','review_ready')`
- High/critical blocker queue:
  - partial index on packages where `risk_tier in ('high','critical') and release_readiness_state in ('not_ready','blocked','conditionally_ready')`
- Gap queue:
  - `(state, severity, opened_at desc)` on feedback records
- Current projection lookup:
  - partial unique `(package_id, projection_kind)` where `is_current=true`
- Latest hidden draft:
  - partial unique `(package_id)` where `is_latest=true`
- Wave ordering:
  - unique `(package_id, publish_order)`

## Политика хранения данных
- Hidden drafts:
  - metadata retained for audit, but raw draft bodies are never stored.
- Evidence blocks, decisions, feedback and artifact links:
  - retained for audit and historical reclassification analysis.
- Projection snapshots:
  - old versions may be compacted later, but current snapshot per kind stays immediately readable.
- PII/secrets:
  - token material, raw auth headers, full prompt transcripts and private draft payloads are forbidden in all tables.

## Доменные инварианты
- `publication_state=hidden_draft` packages are invisible to owner/reviewer/operator projections except internal counts.
- `bundle_admissibility=requires_decomposition` blocks `review_ready` until a valid wave map exists.
- `waiver_state=approved` does not imply `release_readiness_state=ready`.
- `feedback_state=open` can coexist with `release_readiness_state=released`, because postdeploy gaps are explicit lifecycle outcomes.
- `high/critical` packages without approved waiver and residual risk cannot transition to `conditionally_ready` or `ready`.

## Ownership and write path
- `control-plane`:
  - owns all writes to package root, waves, evidence, decisions, feedback and projections.
- `worker`:
  - does not write tables directly; it invokes owner use-cases for sweep/backfill/feedback.
- `agent-runner`:
  - only reports draft/evidence signals through internal callbacks.
- `api-gateway`:
  - validates/authenticates/routs HTTP and webhook inputs; it never computes package state.

## Runtime impact / Migration impact
- Runtime impact (`run:design`): none, docs only.
- Migration impact (`run:dev`):
  - additive tables for package aggregate and ledgers;
  - optional `flow_events` extension with nullable package ref;
  - no schema ownership change beyond `control-plane`.

## Context7 / dependency note
- Новые внешние библиотеки для data-model part не требуются.
