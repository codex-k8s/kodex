-- name: skill_artifact_get :one
SELECT artifact.digest,artifact.size_bytes,artifact.scan_state,artifact.lifecycle_state
FROM control_plane.artifacts artifact
WHERE artifact.organization_id=$1::uuid AND artifact.project_id=$2::uuid
    AND artifact.ref=$3 AND artifact.revision=$4 FOR SHARE;
