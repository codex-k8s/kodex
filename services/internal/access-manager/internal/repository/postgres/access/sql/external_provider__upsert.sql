-- name: external_provider__upsert :exec
INSERT INTO access_external_providers (
    id, slug, provider_kind, display_name, icon_asset_ref, status, version, created_at, updated_at
) VALUES (
    @id, @slug, @provider_kind, @display_name, @icon_asset_ref, @status, @version, @created_at, @updated_at
)
ON CONFLICT (slug) DO UPDATE SET
    provider_kind = EXCLUDED.provider_kind,
    display_name = EXCLUDED.display_name,
    icon_asset_ref = EXCLUDED.icon_asset_ref,
    status = EXCLUDED.status,
    version = EXCLUDED.version,
    updated_at = EXCLUDED.updated_at
WHERE access_external_providers.id = @id;
