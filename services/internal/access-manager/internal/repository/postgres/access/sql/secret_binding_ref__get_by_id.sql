-- name: secret_binding_ref__get_by_id :one
SELECT id, store_type, store_ref, value_fingerprint, rotated_at, version, created_at, updated_at
FROM access_secret_binding_refs
WHERE id = @id;
