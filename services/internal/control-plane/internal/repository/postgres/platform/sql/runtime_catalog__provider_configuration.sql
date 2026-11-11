-- name: runtime_catalog__provider_configuration :one
SELECT capabilities->'modelProvider'
FROM control_plane.provider_definitions
WHERE stable_key = $1 AND enabled;
