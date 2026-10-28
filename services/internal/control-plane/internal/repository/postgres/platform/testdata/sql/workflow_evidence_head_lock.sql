-- name: workflow_evidence_head_lock :one
SELECT id::text FROM control_plane.artifact_heads WHERE ref=$1 FOR UPDATE NOWAIT;
