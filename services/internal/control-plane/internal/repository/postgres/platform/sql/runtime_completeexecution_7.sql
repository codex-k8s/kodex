-- name: platform__runtime_completeexecution_7 :exec
INSERT INTO control_plane.artifact_bindings(artifact_id,target_kind,target_ref,created_by) VALUES($1::uuid,'RUN_RESULT',$2,$3::uuid)
