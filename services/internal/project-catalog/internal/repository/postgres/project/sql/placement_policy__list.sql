-- name: placement_policy__list :many
SELECT
    id, project_id, repository_id, service_key, allowed_cluster_refs,
    status, version, created_at, updated_at
FROM project_catalog_placement_policies
WHERE project_id = @project_id
  AND (@repository_id::uuid IS NULL OR repository_id = @repository_id)
  AND (@service_key = '' OR service_key = @service_key)
  AND (cardinality(@statuses::text[]) = 0 OR status = ANY(@statuses::text[]))
ORDER BY service_key, id
LIMIT @limit OFFSET @offset;
