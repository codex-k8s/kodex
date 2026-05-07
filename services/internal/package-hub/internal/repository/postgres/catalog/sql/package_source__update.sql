-- name: package_source__update :exec
UPDATE package_hub_package_sources
SET
    display_name = @display_name,
    repository_ref = @repository_ref,
    catalog_endpoint_ref = @catalog_endpoint_ref,
    status = @status,
    last_sync_at = @last_sync_at::timestamptz,
    last_error = @last_error,
    version = @version,
    updated_at = @updated_at
WHERE id = @id
  AND version = @previous_version;
