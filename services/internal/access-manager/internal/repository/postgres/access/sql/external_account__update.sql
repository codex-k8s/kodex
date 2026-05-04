-- name: external_account__update :exec
UPDATE access_external_accounts
SET
    external_provider_id = @external_provider_id,
    account_type = @account_type,
    display_name = @display_name,
    image_asset_ref = @image_asset_ref,
    owner_scope_type = @owner_scope_type,
    owner_scope_id = @owner_scope_id,
    status = @status,
    secret_binding_ref_id = @secret_binding_ref_id,
    version = @version,
    updated_at = @updated_at
WHERE id = @id AND version = @previous_version;
