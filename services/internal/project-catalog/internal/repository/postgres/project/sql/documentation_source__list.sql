-- name: documentation_source__list :many
SELECT
    id, project_id, repository_id, scope_type, scope_id, local_path,
    access_mode, status, version, created_at, updated_at
FROM project_catalog_documentation_sources
WHERE project_id = @project_id
  AND (@repository_id::uuid IS NULL OR repository_id = @repository_id)
  AND (@scope_type = '' OR scope_type = @scope_type)
  AND (@scope_id = '' OR scope_id = @scope_id)
  AND (cardinality(@statuses::text[]) = 0 OR status = ANY(@statuses::text[]))
ORDER BY scope_type, scope_id, local_path, id
LIMIT @limit::integer OFFSET @offset::integer;
