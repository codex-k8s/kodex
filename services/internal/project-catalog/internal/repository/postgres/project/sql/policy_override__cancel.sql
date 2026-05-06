-- name: policy_override__cancel :exec
UPDATE project_catalog_policy_overrides
SET
    status = @status,
    version = @version,
    updated_at = @updated_at
WHERE id = @id
  AND project_id = @project_id
  AND version = @previous_version
  AND status = 'active';
