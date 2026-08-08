SELECT plan_id::text, organization_id::text, owner_actor_id::text,
       source_root_reference, source_root_sha256, source_snapshot_sha256,
       idempotency_key_sha256, request_sha256, semantic_sha256,
       project_id::text, state, verification_state, plan_payload,
       operation_count, archived_source_count, prepared_at,
       coalesce(terminal_at, '0001-01-01 00:00:00+00'::timestamptz)
FROM control_plane.legacy_graph_migration_plans
WHERE plan_id = @plan_id::uuid
FOR UPDATE
