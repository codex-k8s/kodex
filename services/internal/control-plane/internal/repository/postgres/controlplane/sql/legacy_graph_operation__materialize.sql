UPDATE control_plane.legacy_graph_operation_receipts
SET target_version = @target_version,
    target_state = @target_state,
    projection_sha256 = @projection_sha256,
    provenance_evidence_sha256 = @provenance_evidence_sha256,
    audit_ids = @audit_ids::uuid[],
    event_ids = @event_ids::uuid[],
    event_sequences = @event_sequences::bigint[],
    materialized_at = @materialized_at
WHERE plan_id = @plan_id::uuid
  AND ordinal = @ordinal
  AND operation_kind = @operation_kind
  AND input_sha256 = @input_sha256
  AND target_id = @target_id::uuid
  AND target_kind = @target_kind
  AND provenance_sha256 = @provenance_sha256
  AND materialized_at IS NULL
