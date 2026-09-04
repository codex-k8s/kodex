-- name: secret_rebind_source_version :one
SELECT revision.version_number FROM control_plane.runtime_environment_versions revision
JOIN control_plane.runtime_environment_sets environment ON environment.id=revision.environment_set_id
WHERE environment.organization_id=$1::uuid AND environment.ref=$2 AND revision.ref=$3;
