-- name: package_installation__update :exec
UPDATE package_hub_package_installations
SET package_version_id = @package_version_id,
    installation_status = @installation_status,
    desired_state = @desired_state,
    runtime_requirement_digest = @runtime_requirement_digest,
    secret_binding_status = @secret_binding_status,
    last_health_status = @last_health_status,
    version = @version,
    updated_at = @updated_at
WHERE id = @id
  AND version = @previous_version;
