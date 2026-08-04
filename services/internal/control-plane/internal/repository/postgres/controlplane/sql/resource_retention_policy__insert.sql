-- name: ResourceRetentionPolicyInsert :exec
INSERT INTO control_plane.resource_retention_policies (
    organization_id, project_id, policy_id, version,
    pvc_retention_seconds, archive_retention_seconds,
    effective_at, actor_id, reason_code,
    idempotency_key_sha256, request_sha256, supersedes_version, created_at
) VALUES (
    @organization_id::uuid, @project_id::uuid, @policy_id, @version,
    @pvc_retention_seconds, @archive_retention_seconds,
    @effective_at, @actor_id::uuid, @reason_code,
    @idempotency_key_sha256, @request_sha256,
    nullif(@supersedes_version, 0), @created_at
);
