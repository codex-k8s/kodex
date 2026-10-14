-- name: artifacts_changeartifactbinding_update_artifacts_version :exec
UPDATE control_plane.artifact_heads SET version=version+1 WHERE id=$1::uuid
