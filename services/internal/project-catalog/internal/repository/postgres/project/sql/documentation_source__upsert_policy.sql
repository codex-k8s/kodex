-- name: documentation_source__upsert_policy :exec
INSERT INTO project_catalog_documentation_sources (
    id, project_id, repository_id, scope_type, scope_id, local_path,
    access_mode, status, managed_by_policy, version, created_at, updated_at
) VALUES (
    @id, @project_id, @repository_id, @scope_type, @scope_id, @local_path,
    @access_mode, @status, true, @version, @created_at, @updated_at
)
ON CONFLICT (project_id, scope_type, scope_id, local_path)
DO UPDATE SET
    repository_id = EXCLUDED.repository_id,
    access_mode = EXCLUDED.access_mode,
    status = EXCLUDED.status,
    managed_by_policy = true,
    version = project_catalog_documentation_sources.version + 1,
    updated_at = EXCLUDED.updated_at;
