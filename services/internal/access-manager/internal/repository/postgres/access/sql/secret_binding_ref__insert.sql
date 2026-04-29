-- name: secret_binding_ref__insert :exec
INSERT INTO access_secret_binding_refs (
    id, store_type, store_ref, value_fingerprint, rotated_at, version, created_at, updated_at
) VALUES (
    @id, @store_type, @store_ref, @value_fingerprint, @rotated_at, @version, @created_at, @updated_at
);
