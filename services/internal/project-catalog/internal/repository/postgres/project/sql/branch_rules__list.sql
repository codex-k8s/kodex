-- name: branch_rules__list :many
SELECT
    id, project_id, repository_id, pattern, required_checks, merge_policy,
    status, version, created_at, updated_at
FROM project_catalog_branch_rules
WHERE project_id = @project_id
  AND (@repository_id::uuid IS NULL OR repository_id = @repository_id)
  AND (cardinality(@statuses::text[]) = 0 OR status = ANY(@statuses::text[]))
ORDER BY pattern, id
LIMIT @limit::integer OFFSET @offset::integer;
