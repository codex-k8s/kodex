-- name: release_line__upsert :exec
INSERT INTO project_catalog_release_lines (
    id, project_id, release_policy_id, name, branch_pattern, status,
    version, created_at, updated_at
) VALUES (
    @id, @project_id, @release_policy_id, @name, @branch_pattern, @status,
    @version, @created_at, @updated_at
)
ON CONFLICT (id) DO UPDATE SET
    release_policy_id = EXCLUDED.release_policy_id,
    name = EXCLUDED.name,
    branch_pattern = EXCLUDED.branch_pattern,
    status = EXCLUDED.status,
    version = EXCLUDED.version,
    updated_at = EXCLUDED.updated_at
WHERE project_catalog_release_lines.project_id = EXCLUDED.project_id;
