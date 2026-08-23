-- name: commands_changeagentbinding_select_enabled_capability :one
SELECT stable_key
FROM control_plane.platform_capabilities
WHERE stable_key = $1
  AND enabled
