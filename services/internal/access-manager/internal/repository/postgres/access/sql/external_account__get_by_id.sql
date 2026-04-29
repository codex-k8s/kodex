-- name: external_account__get_by_id :one
SELECT id, external_provider_id, account_type, display_name, image_asset_ref,
       owner_scope_type, owner_scope_id, status, secret_binding_ref_id,
       version, created_at, updated_at
FROM access_external_accounts
WHERE id = @id;
