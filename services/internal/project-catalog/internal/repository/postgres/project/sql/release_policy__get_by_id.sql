-- name: release_policy__get_by_id :one
SELECT
    id, project_id, name, branch_pattern, rollout_strategy, rollback_policy,
    risk_profile_ref, status, version, created_at, updated_at
FROM project_catalog_release_policies
WHERE id = @id;
