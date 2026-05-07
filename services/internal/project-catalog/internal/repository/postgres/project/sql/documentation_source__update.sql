-- name: documentation_source__update :exec
UPDATE project_catalog_documentation_sources
SET
    repository_id = @repository_id,
    scope_type = @scope_type,
    scope_id = @scope_id,
    local_path = @local_path,
    access_mode = @access_mode,
    status = @status,
    managed_by_policy = false,
    version = @version,
    updated_at = @updated_at
WHERE id = @id
  AND project_id = @project_id
  AND version = @previous_version;
