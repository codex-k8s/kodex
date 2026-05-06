-- name: documentation_source__create :exec
INSERT INTO project_catalog_documentation_sources (
    id, project_id, repository_id, scope_type, scope_id, local_path,
    access_mode, status, version, created_at, updated_at
) VALUES (
    @id, @project_id, @repository_id, @scope_type, @scope_id, @local_path,
    @access_mode, @status, @version, @created_at, @updated_at
);
