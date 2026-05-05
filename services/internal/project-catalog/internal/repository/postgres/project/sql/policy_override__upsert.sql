-- name: policy_override__upsert :exec
INSERT INTO project_catalog_policy_overrides (
    id, project_id, target_type, target_id, payload, reason, status,
    expires_at, created_by_actor_ref, version, created_at, updated_at
) VALUES (
    @id, @project_id, @target_type, @target_id, @payload::jsonb, @reason, @status,
    @expires_at, @created_by_actor_ref, @version, @created_at, @updated_at
)
ON CONFLICT (id) DO UPDATE SET
    payload = EXCLUDED.payload,
    reason = EXCLUDED.reason,
    status = EXCLUDED.status,
    expires_at = EXCLUDED.expires_at,
    version = EXCLUDED.version,
    updated_at = EXCLUDED.updated_at
WHERE project_catalog_policy_overrides.project_id = EXCLUDED.project_id;
