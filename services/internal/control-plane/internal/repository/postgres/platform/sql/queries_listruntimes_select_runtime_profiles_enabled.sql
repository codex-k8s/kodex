-- name: queries_listruntimes_select_runtime_profiles_enabled :many
SELECT stable_key, name, provider, model, runtime_revision
FROM control_plane.runtime_profiles
WHERE enabled
ORDER BY name, stable_key
