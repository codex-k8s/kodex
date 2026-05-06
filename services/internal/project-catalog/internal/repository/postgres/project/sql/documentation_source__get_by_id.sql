-- name: documentation_source__get_by_id :one
SELECT
    id, project_id, repository_id, scope_type, scope_id, local_path,
    access_mode, status, version, created_at, updated_at
FROM project_catalog_documentation_sources
WHERE id = @id;
