-- name: policy_override__get_by_id :one
SELECT
    id,
    project_id,
    target_type,
    target_id,
    payload,
    reason,
    status,
    expires_at,
    created_by_actor_ref,
    version,
    created_at,
    updated_at
FROM project_catalog_policy_overrides
WHERE id = @id;
