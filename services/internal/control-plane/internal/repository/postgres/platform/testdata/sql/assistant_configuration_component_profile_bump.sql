-- name: assistant_configuration_component_profile_bump :exec
UPDATE control_plane.runtime_profiles SET version=version+1,runtime_revision=runtime_revision || '-fixture-forward'
WHERE stable_key=$1;
