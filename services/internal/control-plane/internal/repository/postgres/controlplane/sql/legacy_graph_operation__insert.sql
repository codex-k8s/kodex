INSERT INTO control_plane.legacy_graph_operation_receipts (
    plan_id, ordinal, operation_kind, input_sha256, target_id,
    target_kind, provenance_sha256
) VALUES (
    @plan_id::uuid, @ordinal, @operation_kind, @input_sha256,
    @target_id::uuid, @target_kind, @provenance_sha256
)
