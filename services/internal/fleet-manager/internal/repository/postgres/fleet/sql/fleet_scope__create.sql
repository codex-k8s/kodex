-- name: fleet_scope__create :exec
INSERT INTO fleet_manager_scopes (
    id, scope_key, scope_type, scope_owner_id, owner_ref_json, display_name,
    status, is_default, created_at, updated_at, version
) VALUES (
    @id, @scope_key, @scope_type, @scope_owner_id, @owner_ref_json, @display_name,
    @status, @is_default, @created_at, @updated_at, @version
);
