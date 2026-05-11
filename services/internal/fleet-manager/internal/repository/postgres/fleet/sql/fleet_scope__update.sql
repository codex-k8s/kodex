-- name: fleet_scope__update :exec
UPDATE fleet_manager_scopes
SET
    scope_key = @scope_key,
    scope_type = @scope_type,
    scope_owner_id = @scope_owner_id,
    owner_ref_json = @owner_ref_json,
    display_name = @display_name,
    status = @status,
    is_default = @is_default,
    updated_at = @updated_at,
    version = @version
WHERE id = @id AND version = @previous_version;
