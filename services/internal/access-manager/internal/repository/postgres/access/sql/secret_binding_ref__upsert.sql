-- name: secret_binding_ref__upsert :exec
INSERT INTO access_secret_binding_refs (
    id, store_type, store_ref, value_fingerprint, rotated_at, version, created_at, updated_at
) VALUES (
    @id, @store_type, @store_ref, @value_fingerprint, @rotated_at, @version, @created_at, @updated_at
)
ON CONFLICT (id) DO UPDATE SET
    store_type = EXCLUDED.store_type,
    store_ref = EXCLUDED.store_ref,
    value_fingerprint = EXCLUDED.value_fingerprint,
    rotated_at = EXCLUDED.rotated_at,
    version = EXCLUDED.version,
    updated_at = EXCLUDED.updated_at;
