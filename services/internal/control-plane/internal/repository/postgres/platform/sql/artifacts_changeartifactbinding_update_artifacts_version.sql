-- name: platform__artifacts_changeartifactbinding_update_artifacts_version :exec
UPDATE control_plane.artifacts SET version=version+1 WHERE id=$1::uuid
