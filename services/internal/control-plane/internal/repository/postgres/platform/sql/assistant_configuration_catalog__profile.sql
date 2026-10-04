-- name: assistant_configuration_catalog__profile :one
SELECT provider FROM control_plane.runtime_profiles WHERE enabled AND stable_key=@profile_ref;
