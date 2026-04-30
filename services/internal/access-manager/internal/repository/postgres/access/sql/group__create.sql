-- name: group__create :exec
INSERT INTO access_groups (
    id, scope_type, scope_id, slug, display_name, parent_group_id, image_asset_ref,
    status, version, created_at, updated_at
) VALUES (
    @id, @scope_type, @scope_id, @slug, @display_name, @parent_group_id, @image_asset_ref,
    @status, @version, @created_at, @updated_at
);
