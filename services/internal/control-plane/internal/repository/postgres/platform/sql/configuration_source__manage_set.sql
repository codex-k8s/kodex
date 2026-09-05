-- name: configuration_source__manage_set :one
UPDATE control_plane.managed_configuration_sets
SET managed_by='GIT',source=$2,source_revision=$3,version=version+1,updated_at=clock_timestamp()
WHERE id=$1::uuid AND version=$4 RETURNING version,updated_at;
