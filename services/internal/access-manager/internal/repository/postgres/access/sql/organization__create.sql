-- name: organization__create :exec
INSERT INTO access_organizations (
    id, kind, slug, display_name, image_asset_ref, status, parent_organization_id,
    version, created_at, updated_at
) VALUES (
    @id, @kind, @slug, @display_name, @image_asset_ref, @status, @parent_organization_id,
    @version, @created_at, @updated_at
);
