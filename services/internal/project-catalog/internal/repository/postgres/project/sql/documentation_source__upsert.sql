-- name: documentation_source__upsert :exec
INSERT INTO project_catalog_documentation_sources (
    id, project_id, repository_id, scope_type, scope_id, local_path,
    access_mode, status, version, created_at, updated_at
) VALUES (
    @id, @project_id, @repository_id, @scope_type, @scope_id, @local_path,
    @access_mode, @status, @version, @created_at, @updated_at
)
ON CONFLICT (id) DO UPDATE SET
    repository_id = EXCLUDED.repository_id,
    scope_type = EXCLUDED.scope_type,
    scope_id = EXCLUDED.scope_id,
    local_path = EXCLUDED.local_path,
    access_mode = EXCLUDED.access_mode,
    status = EXCLUDED.status,
    version = EXCLUDED.version,
    updated_at = EXCLUDED.updated_at
WHERE project_catalog_documentation_sources.project_id = EXCLUDED.project_id;
