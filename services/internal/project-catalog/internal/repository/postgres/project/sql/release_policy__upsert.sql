-- name: release_policy__upsert :exec
INSERT INTO project_catalog_release_policies (
    id, project_id, name, branch_pattern, rollout_strategy, rollback_policy,
    risk_profile_ref, status, version, created_at, updated_at
) VALUES (
    @id, @project_id, @name, @branch_pattern, @rollout_strategy, @rollback_policy,
    @risk_profile_ref, @status, @version, @created_at, @updated_at
)
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name,
    branch_pattern = EXCLUDED.branch_pattern,
    rollout_strategy = EXCLUDED.rollout_strategy,
    rollback_policy = EXCLUDED.rollback_policy,
    risk_profile_ref = EXCLUDED.risk_profile_ref,
    status = EXCLUDED.status,
    version = EXCLUDED.version,
    updated_at = EXCLUDED.updated_at
WHERE project_catalog_release_policies.project_id = EXCLUDED.project_id;
