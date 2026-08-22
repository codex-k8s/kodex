-- name: platform__commands_resolve_enabled_runtime_profile :one
SELECT stable_key, name, provider, model, runtime_revision
FROM control_plane.runtime_profiles
WHERE stable_key = $1
  AND enabled
