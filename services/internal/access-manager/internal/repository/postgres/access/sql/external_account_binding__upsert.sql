-- name: external_account_binding__upsert :exec
INSERT INTO access_external_account_bindings (
    id, external_account_id, usage_scope_type, usage_scope_id, allowed_action_keys,
    status, version, created_at, updated_at
) VALUES (
    @id, @external_account_id, @usage_scope_type, @usage_scope_id, @allowed_action_keys,
    @status, @version, @created_at, @updated_at
)
ON CONFLICT (external_account_id, usage_scope_type, usage_scope_id) DO UPDATE SET
    id = EXCLUDED.id,
    allowed_action_keys = EXCLUDED.allowed_action_keys,
    status = EXCLUDED.status,
    version = EXCLUDED.version,
    created_at = EXCLUDED.created_at,
    updated_at = EXCLUDED.updated_at;
