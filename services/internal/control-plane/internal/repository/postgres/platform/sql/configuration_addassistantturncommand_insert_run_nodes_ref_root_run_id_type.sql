-- name: configuration_addassistantturncommand_insert_run_nodes_ref_root_run_id_type :exec
INSERT INTO control_plane.run_nodes(ref,organization_id,root_run_id,run_id,turn_id,type,state,display_name,role,agent_id,input_summary,next_actions)
SELECT $1,$2::uuid,$3::uuid,$3::uuid,$4::uuid,'AGENT_EXECUTION','QUEUED',a.name,a.role_description,a.id,$5,ARRAY['OPEN','CANCEL']
FROM control_plane.agents a
WHERE a.organization_id=$2::uuid AND a.system_key='system-assistant'
