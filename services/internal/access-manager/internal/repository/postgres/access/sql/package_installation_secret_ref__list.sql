-- name: package_installation_secret_ref__list :many
SELECT
    r.id,
    r.package_installation_id,
    r.installation_scope_type,
    r.installation_scope_id,
    r.logical_key,
    r.status,
    r.metadata,
    r.version,
    r.created_at,
    r.updated_at,
    s.id,
    s.store_type,
    s.store_ref,
    s.value_fingerprint,
    s.rotated_at,
    s.version,
    s.created_at,
    s.updated_at
FROM access_package_installation_secret_refs r
JOIN access_secret_binding_refs s ON s.id = r.secret_binding_ref_id
WHERE r.package_installation_id = @package_installation_id
  AND r.installation_scope_type = @installation_scope_type
  AND r.installation_scope_id = @installation_scope_id
  AND (cardinality(@logical_keys::text[]) = 0 OR r.logical_key = ANY(@logical_keys::text[]))
ORDER BY r.logical_key ASC;
