-- name: external_account_binding__get_by_id :one
SELECT id, external_account_id, usage_scope_type, usage_scope_id, allowed_action_keys,
       status, version, created_at, updated_at
FROM access_external_account_bindings
WHERE id = @id;
