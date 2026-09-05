-- name: configuration_source__accept_set :exec
UPDATE control_plane.managed_configuration_sets SET source_revision=$2 WHERE id=$1::uuid AND managed_by='GIT';
