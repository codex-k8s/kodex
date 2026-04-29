-- name: external_provider__create :exec
INSERT INTO access_external_providers (
    id, slug, provider_kind, display_name, icon_asset_ref, status, version, created_at, updated_at
) VALUES (
    @id, @slug, @provider_kind, @display_name, @icon_asset_ref, @status, @version, @created_at, @updated_at
);
