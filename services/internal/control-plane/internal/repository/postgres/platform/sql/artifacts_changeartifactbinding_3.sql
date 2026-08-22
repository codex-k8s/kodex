-- name: platform__artifacts_changeartifactbinding_3 :exec
DELETE FROM control_plane.artifact_bindings WHERE artifact_id=$1::uuid AND target_kind='KNOWLEDGE' AND target_ref=$2
