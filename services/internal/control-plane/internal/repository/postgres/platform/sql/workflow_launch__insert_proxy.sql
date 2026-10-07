-- name: workflow_launch__insert_proxy :one
INSERT INTO control_plane.run_nodes(ref,organization_id,root_run_id,run_id,parent_node_id,type,state,display_name,role,input_summary,next_actions)
VALUES(@node_ref,@organization_id::uuid,@root_run_id::uuid,@run_id::uuid,@parent_node_id::uuid,'EXTERNAL_ACTION','WAITING',@name,'Workflow',@child_ref,ARRAY['OPEN']) RETURNING id::text;
