-- name: release_line__get_by_id :one
SELECT
    id, project_id, release_policy_id, name, branch_pattern, status,
    version, created_at, updated_at
FROM project_catalog_release_lines
WHERE id = @id;
