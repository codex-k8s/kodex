-- name: manual_complete :exec
SELECT control_plane.artifact_manual_retention_complete(@artifact_id::uuid,@claim_owner,@claim_generation);
