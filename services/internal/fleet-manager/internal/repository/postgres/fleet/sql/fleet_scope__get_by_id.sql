-- name: fleet_scope__get_by_id :one
SELECT
    id, scope_key, scope_type, scope_owner_id, owner_ref_json, display_name,
    status, is_default, version, created_at, updated_at
FROM fleet_manager_scopes
WHERE id = @id;
