-- name: external_account_binding__update :exec
UPDATE access_external_account_bindings
SET
    allowed_action_keys = @allowed_action_keys,
    status = @status,
    version = @version,
    updated_at = @updated_at
WHERE id = @id AND version = @previous_version;
