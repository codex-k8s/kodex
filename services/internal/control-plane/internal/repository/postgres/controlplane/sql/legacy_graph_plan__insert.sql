INSERT INTO control_plane.legacy_graph_migration_plans (
    plan_id, organization_id, owner_actor_id, source_root_reference,
    source_root_sha256, source_snapshot_sha256, idempotency_key_sha256,
    request_sha256, semantic_sha256, project_id, state, verification_state,
    operation_count, archived_source_count, plan_payload, prepared_at
) VALUES (
    @plan_id::uuid, @organization_id::uuid, @owner_actor_id::uuid,
    @source_root_reference, @source_root_sha256, @source_snapshot_sha256,
    @idempotency_key_sha256, @request_sha256, @semantic_sha256,
    @project_id::uuid, 'PREPARED', 'VERIFIED', @operation_count,
    @archived_source_count, @plan_payload::bytea, @prepared_at
)
ON CONFLICT DO NOTHING
