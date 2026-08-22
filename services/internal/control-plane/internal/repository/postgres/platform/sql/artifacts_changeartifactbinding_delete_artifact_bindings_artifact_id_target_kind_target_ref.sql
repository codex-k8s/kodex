-- name: platform__artifacts_changeartifactbinding_delete_artifact_bindings_artifact_id_target_kind_target_ref :exec
DELETE FROM control_plane.artifact_bindings WHERE artifact_id=$1::uuid AND target_kind='KNOWLEDGE' AND target_ref=$2
