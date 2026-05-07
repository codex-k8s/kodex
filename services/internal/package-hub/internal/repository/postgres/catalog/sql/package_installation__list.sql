-- name: package_installation__list :many
SELECT
    i.id,
    i.package_id,
    i.package_version_id,
    i.scope_type,
    i.scope_ref,
    i.installation_status,
    i.desired_state,
    i.runtime_requirement_digest,
    i.secret_binding_status,
    i.last_health_status,
    i.version,
    i.created_at,
    i.updated_at
FROM package_hub_package_installations i
JOIN package_hub_packages p ON p.id = i.package_id
WHERE (@scope_type::text IS NULL OR i.scope_type = @scope_type::text)
  AND (@scope_ref::text IS NULL OR i.scope_ref = @scope_ref::text)
  AND (@package_id::uuid IS NULL OR i.package_id = @package_id::uuid)
  AND (@package_kind::text IS NULL OR p.package_kind = @package_kind::text)
  AND (@installation_status::text IS NULL OR i.installation_status = @installation_status::text)
  AND (@secret_binding_status::text IS NULL OR i.secret_binding_status = @secret_binding_status::text)
ORDER BY i.updated_at DESC, i.id
LIMIT @limit::integer
OFFSET @offset::bigint;
