-- name: service_descriptor__list :many
SELECT
    id, project_id, services_policy_id, repository_id, service_key,
    display_name, kind, root_path, documentation_scope_id,
    depends_on_service_keys, status, version, created_at, updated_at
FROM project_catalog_service_descriptors
WHERE project_id = @project_id
  AND (@repository_id::uuid IS NULL OR repository_id = @repository_id)
  AND (cardinality(@service_keys::text[]) = 0 OR service_key = ANY(@service_keys::text[]))
  AND (cardinality(@statuses::text[]) = 0 OR status = ANY(@statuses::text[]))
ORDER BY service_key, id
LIMIT @limit OFFSET @offset;
