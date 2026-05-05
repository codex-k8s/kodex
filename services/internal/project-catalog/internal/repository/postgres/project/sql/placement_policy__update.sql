-- name: placement_policy__update :exec
UPDATE project_catalog_placement_policies
SET
    repository_id = @repository_id,
    service_key = @service_key,
    allowed_cluster_refs = @allowed_cluster_refs,
    status = @status,
    version = @version,
    updated_at = @updated_at
WHERE id = @id
  AND project_id = @project_id
  AND version = @previous_version;
