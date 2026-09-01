-- name: access_bootstrap_insert_permission :exec
INSERT INTO control_plane.permission_registry
    (permission_key, name_key, description_key, risk, allowed_scopes, resource_kinds, owner_condition_supported)
VALUES (@permission_key, @name_key, @description_key, @risk, @allowed_scopes::text[], @resource_kinds::text[], @owner_condition_supported)
ON CONFLICT (permission_key) DO NOTHING
