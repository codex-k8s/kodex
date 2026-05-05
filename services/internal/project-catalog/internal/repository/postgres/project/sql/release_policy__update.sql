-- name: release_policy__update :exec
UPDATE project_catalog_release_policies
SET
    name = @name,
    branch_pattern = @branch_pattern,
    rollout_strategy = @rollout_strategy,
    rollback_policy = @rollback_policy,
    risk_profile_ref = @risk_profile_ref,
    status = @status,
    version = @version,
    updated_at = @updated_at
WHERE id = @id
  AND project_id = @project_id
  AND version = @previous_version;
