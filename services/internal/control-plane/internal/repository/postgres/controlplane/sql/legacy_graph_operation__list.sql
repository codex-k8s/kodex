SELECT plan_id::text, ordinal, operation_kind, input_sha256, target_id::text,
       target_kind, target_version, coalesce(target_state, ''),
       coalesce(projection_sha256, ''), provenance_sha256,
       coalesce(provenance_evidence_sha256, ''),
       audit_ids::text[], event_ids::text[], event_sequences,
       coalesce(materialized_at, '0001-01-01 00:00:00+00'::timestamptz)
FROM control_plane.legacy_graph_operation_receipts
WHERE plan_id = @plan_id::uuid
ORDER BY ordinal
