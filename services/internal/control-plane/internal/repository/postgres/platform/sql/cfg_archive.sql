-- name: cfg_archive :one
UPDATE control_plane.managed_configuration_sets
SET archived=$3,version=version+1,updated_at=clock_timestamp()
WHERE id=$1::uuid AND version=$2 AND managed_by='UI'
RETURNING version,updated_at
