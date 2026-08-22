-- name: platform__artifacts_downloadartifact_1 :one
SELECT c.body FROM control_plane.artifact_content c JOIN control_plane.artifacts a ON a.id=c.artifact_id WHERE a.organization_id=$1::uuid AND a.ref=$2
