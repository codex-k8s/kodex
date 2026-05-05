-- name: release_line__update :exec
UPDATE project_catalog_release_lines
SET
    release_policy_id = @release_policy_id,
    name = @name,
    branch_pattern = @branch_pattern,
    status = @status,
    version = @version,
    updated_at = @updated_at
WHERE id = @id
  AND project_id = @project_id
  AND version = @previous_version;
