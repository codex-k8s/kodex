-- name: kubernetes_capture__artifact_binding :exec
INSERT INTO control_plane.artifact_bindings(artifact_id,target_kind,target_ref,created_by)
SELECT @artifact_id::uuid,'RUN_RESULT',@run_ref,@actor_id::uuid
WHERE NOT EXISTS(SELECT 1 FROM control_plane.artifact_bindings binding
 WHERE artifact_id=@artifact_id::uuid AND target_kind='RUN_RESULT' AND target_ref=@run_ref);
