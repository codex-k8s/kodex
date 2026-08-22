-- name: platform__runtime_delegateexecution_4 :one
INSERT INTO control_plane.runs(ref,organization_id,project_id,session_id,root_run_id,parent_run_id,target_type,target_ref,source,title,task,input,state,initiated_by,started_at) VALUES($1,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::uuid,'AGENT',$7,'AGENT_DELEGATION',$8,$9,$10,'RUNNING',$11::uuid,clock_timestamp()) RETURNING id::text
