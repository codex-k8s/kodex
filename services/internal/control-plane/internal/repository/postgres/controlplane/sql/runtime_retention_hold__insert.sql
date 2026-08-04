-- name: RuntimeRetentionHoldInsert :exec
INSERT INTO control_plane.runtime_retention_holds (
    organization_id, project_id, session_id, hold_id, kind, state, version,
    actor_id, reason_code, idempotency_key_sha256, request_sha256,
    created_at, updated_at
) VALUES (
    @organization_id::uuid, @project_id::uuid, @session_id::uuid,
    @hold_id::uuid, @kind, 'ACTIVE', 1,
    @actor_id::uuid, @reason_code, @idempotency_key_sha256, @request_sha256,
    @created_at, @created_at
);
