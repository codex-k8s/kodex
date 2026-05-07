-- name: package_installation__get_by_id :one
SELECT
    id,
    package_id,
    package_version_id,
    scope_type,
    scope_ref,
    installation_status,
    desired_state,
    runtime_requirement_digest,
    secret_binding_status,
    last_health_status,
    version,
    created_at,
    updated_at
FROM package_hub_package_installations
WHERE id = @id;
