-- name: external_account__create :exec
INSERT INTO access_external_accounts (
    id, external_provider_id, account_type, display_name, image_asset_ref,
    owner_scope_type, owner_scope_id, status, secret_binding_ref_id,
    version, created_at, updated_at
) VALUES (
    @id, @external_provider_id, @account_type, @display_name, @image_asset_ref,
    @owner_scope_type, @owner_scope_id, @status, @secret_binding_ref_id,
    @version, @created_at, @updated_at
);
