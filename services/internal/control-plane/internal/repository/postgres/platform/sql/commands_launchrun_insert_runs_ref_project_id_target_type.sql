-- name: platform__commands_launchrun_insert_runs_ref_project_id_target_type :one
INSERT INTO control_plane.runs(ref,organization_id,project_id,session_id,target_type,target_ref,source,title,task,input,input_artifact_refs,state,initiated_by,concurrency_limit) VALUES($1,$2::uuid,$3::uuid,$4::uuid,$5,$6,$7,$8,$9,$10,$11,'QUEUED',$12::uuid,$13) RETURNING id::text
