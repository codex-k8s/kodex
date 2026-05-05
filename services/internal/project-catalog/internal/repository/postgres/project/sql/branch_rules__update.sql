-- name: branch_rules__update :exec
UPDATE project_catalog_branch_rules
SET
    repository_id = @repository_id,
    pattern = @pattern,
    required_checks = @required_checks,
    merge_policy = @merge_policy,
    status = @status,
    version = @version,
    updated_at = @updated_at
WHERE id = @id
  AND project_id = @project_id
  AND version = @previous_version;
