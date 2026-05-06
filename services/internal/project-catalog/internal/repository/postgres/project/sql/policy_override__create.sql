-- name: policy_override__create :exec
INSERT INTO project_catalog_policy_overrides (
    id, project_id, target_type, target_id, payload, reason, status,
    expires_at, created_by_actor_ref, version, created_at, updated_at
) VALUES (
    @id, @project_id, @target_type, @target_id, @payload::jsonb, @reason, @status,
    @expires_at, @created_by_actor_ref, @version, @created_at, @updated_at
);
