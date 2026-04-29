-- name: organization__get_by_id :one
SELECT id, kind, slug, display_name, image_asset_ref, status, parent_organization_id, version, created_at, updated_at
FROM access_organizations
WHERE id = @id;
