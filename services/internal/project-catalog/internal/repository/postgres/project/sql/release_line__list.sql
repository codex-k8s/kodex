-- name: release_line__list :many
SELECT
    id, project_id, release_policy_id, name, branch_pattern, status,
    version, created_at, updated_at
FROM project_catalog_release_lines
WHERE project_id = @project_id
  AND (@release_policy_id::uuid IS NULL OR release_policy_id = @release_policy_id)
  AND (cardinality(@statuses::text[]) = 0 OR status = ANY(@statuses::text[]))
ORDER BY name, id
LIMIT @limit OFFSET @offset;
