-- name: external_provider__get_by_id :one
SELECT id, slug, provider_kind, display_name, icon_asset_ref, status, version, created_at, updated_at
FROM access_external_providers
WHERE id = @id;
