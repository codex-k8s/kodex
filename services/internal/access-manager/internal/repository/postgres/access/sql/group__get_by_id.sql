-- name: group__get_by_id :one
SELECT id, scope_type, scope_id, slug, display_name, parent_group_id, image_asset_ref,
       status, version, created_at, updated_at
FROM access_groups
WHERE id = @id;
