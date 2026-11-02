-- name: assistant_configuration_catalog__profile :one
SELECT provider FROM control_plane.runtime_profiles WHERE enabled AND EXISTS (SELECT 1 FROM control_plane.provider_definitions definition WHERE definition.stable_key = control_plane.runtime_profiles.provider AND definition.enabled) AND stable_key=@profile_ref;
