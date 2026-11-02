-- name: queries_listruntimes_select_runtime_profiles_enabled :many
SELECT stable_key, name, provider, model, runtime_revision
FROM control_plane.runtime_profiles
WHERE enabled AND EXISTS (SELECT 1 FROM control_plane.provider_definitions definition WHERE definition.stable_key = control_plane.runtime_profiles.provider AND definition.enabled)
ORDER BY name, stable_key
