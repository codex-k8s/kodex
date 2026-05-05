-- name: branch_rules__upsert :exec
INSERT INTO project_catalog_branch_rules (
    id, project_id, repository_id, pattern, required_checks, merge_policy,
    status, version, created_at, updated_at
) VALUES (
    @id, @project_id, @repository_id, @pattern, @required_checks, @merge_policy,
    @status, @version, @created_at, @updated_at
)
ON CONFLICT (id) DO UPDATE SET
    repository_id = EXCLUDED.repository_id,
    pattern = EXCLUDED.pattern,
    required_checks = EXCLUDED.required_checks,
    merge_policy = EXCLUDED.merge_policy,
    status = EXCLUDED.status,
    version = EXCLUDED.version,
    updated_at = EXCLUDED.updated_at
WHERE project_catalog_branch_rules.project_id = EXCLUDED.project_id;
