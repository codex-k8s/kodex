-- name: workflow_exception__manager :one
WITH manager AS (INSERT INTO control_plane.run_nodes(ref,organization_id,root_run_id,run_id,parent_node_id,type,state,
    agent_id,turn_id,display_name,role,workflow_step_key,attempt,next_actions)
SELECT @node_ref,root.organization_id,root.id,@run_id::uuid,parent.id,'AGENT_EXECUTION','QUEUED',
    @agent_id::uuid,@turn_id::uuid,@display_name,@role,@step_key,1,ARRAY['OPEN','CANCEL']
FROM control_plane.runs root
JOIN control_plane.run_nodes parent ON parent.organization_id=root.organization_id AND parent.root_run_id=root.id
    AND parent.parent_node_id IS NULL AND parent.type='ROOT_PROCESS'
WHERE root.organization_id=@organization_id::uuid AND root.id=@root_run_id::uuid AND root.root_run_id=root.id
RETURNING id,parent_node_id,organization_id,root_run_id), edge AS (
 INSERT INTO control_plane.run_edges(ref,organization_id,root_run_id,source_node_id,target_node_id,type,label)
 SELECT @edge_ref,organization_id,root_run_id,parent_node_id,id,'DELEGATED_TO','' FROM manager RETURNING target_node_id
)
SELECT manager.id::text FROM manager JOIN edge ON edge.target_node_id=manager.id;
