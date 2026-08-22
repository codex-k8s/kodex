-- name: platform__commands_launchrun_9 :one
INSERT INTO control_plane.run_nodes(ref,organization_id,root_run_id,run_id,type,state,display_name,role,turn_id,input_summary,next_actions) VALUES($1,$2::uuid,$3::uuid,$3::uuid,'ROOT_PROCESS','RUNNING',$4,'Координация',$5::uuid,$6,ARRAY['OPEN','CANCEL']) RETURNING id::text
