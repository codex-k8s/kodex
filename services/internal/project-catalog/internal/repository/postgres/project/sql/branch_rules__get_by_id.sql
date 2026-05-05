-- name: branch_rules__get_by_id :one
SELECT
    id, project_id, repository_id, pattern, required_checks, merge_policy,
    status, version, created_at, updated_at
FROM project_catalog_branch_rules
WHERE id = @id;
