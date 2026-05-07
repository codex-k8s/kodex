-- name: documentation_source__mark_policy_managed_disabled :exec
UPDATE project_catalog_documentation_sources
SET
    status = 'disabled',
    version = version + 1,
    updated_at = @updated_at
WHERE project_id = @project_id::uuid
  AND managed_by_policy = true
  AND status = 'active';
