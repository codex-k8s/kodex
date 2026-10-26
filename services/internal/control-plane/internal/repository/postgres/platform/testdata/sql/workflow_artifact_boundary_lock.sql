-- name: workflow_artifact_boundary_lock :exec
SELECT id FROM control_plane.artifact_heads WHERE ref=@artifact_ref FOR UPDATE;
