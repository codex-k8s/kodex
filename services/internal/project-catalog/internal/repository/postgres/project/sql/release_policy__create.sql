-- name: release_policy__create :exec
INSERT INTO project_catalog_release_policies (
    id, project_id, name, branch_pattern, rollout_strategy, rollback_policy,
    risk_profile_ref, status, version, created_at, updated_at
) VALUES (
    @id, @project_id, @name, @branch_pattern, @rollout_strategy, @rollback_policy,
    @risk_profile_ref, @status, @version, @created_at, @updated_at
);
