-- name: runtime_completeexecution_insert_artifact_bindings_artifact_id_target_ref :exec
INSERT INTO control_plane.artifact_bindings(artifact_id,target_kind,target_ref,created_by) VALUES($1::uuid,'RUN_RESULT',$2,$3::uuid)
