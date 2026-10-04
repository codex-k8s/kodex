-- name: assistant_configuration__profile_pin :one
SELECT stable_key,version,runtime_revision,enabled
FROM control_plane.runtime_profiles WHERE stable_key=@profile_ref FOR SHARE;
