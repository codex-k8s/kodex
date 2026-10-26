-- name: legacy_workflow_rework__insert_node :one
INSERT INTO control_plane.run_nodes(ref,organization_id,root_run_id,run_id,parent_node_id,type,state,
    display_name,role,agent_id,turn_id,workflow_step_key,human_gate_after,attempt,input_summary,next_actions,owner_rework_gate_id)
SELECT @node_ref,prior.organization_id,prior.root_run_id,@run_id::uuid,@parent_node_id::uuid,'AGENT_EXECUTION','QUEUED',
    prior.display_name,prior.role,prior.agent_id,@turn_id::uuid,@step_key,prior.human_gate_after,
    prior.attempt+1,@input_summary,ARRAY['OPEN','CANCEL'],@gate_id::uuid
FROM control_plane.run_nodes prior WHERE prior.id=@prior_node_id::uuid AND prior.organization_id=@organization_id::uuid
    AND prior.state='SUCCEEDED' AND prior.attempt<2147483647
RETURNING id::text;
