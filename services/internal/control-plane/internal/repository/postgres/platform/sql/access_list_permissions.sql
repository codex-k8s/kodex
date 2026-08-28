-- name: access_list_permissions :many
SELECT permission_key, name_key, description_key, risk, allowed_scopes,
       resource_kinds, owner_condition_supported
FROM control_plane.permission_registry
ORDER BY permission_key
