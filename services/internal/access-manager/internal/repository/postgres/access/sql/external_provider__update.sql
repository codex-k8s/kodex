-- name: external_provider__update :exec
UPDATE access_external_providers
SET
    slug = @slug,
    provider_kind = @provider_kind,
    display_name = @display_name,
    icon_asset_ref = @icon_asset_ref,
    status = @status,
    version = @version,
    updated_at = @updated_at
WHERE id = @id AND version = @previous_version;
