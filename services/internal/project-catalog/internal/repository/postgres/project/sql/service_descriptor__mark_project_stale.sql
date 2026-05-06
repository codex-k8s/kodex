-- name: service_descriptor__mark_project_stale :exec
UPDATE project_catalog_service_descriptors
SET
    status = 'stale',
    version = version + 1,
    updated_at = @updated_at
WHERE project_id = @project_id
  AND status <> 'stale';
