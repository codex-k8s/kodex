-- name: commands_launchrun_insert_planned_workflow_node :one
INSERT INTO control_plane.run_nodes(
    ref,organization_id,root_run_id,run_id,parent_node_id,type,state,display_name,role,
    agent_id,workflow_step_key,human_gate_after,input_summary,next_actions,materialization_state
)
SELECT @node_ref,@organization_id::uuid,@root_run_id::uuid,@root_run_id::uuid,
       @parent_node_id::uuid,'AGENT_EXECUTION','PLANNED',agent.name,agent.role_description,
       agent.id,@workflow_step_key,@human_gate_after,@input_summary,ARRAY['OPEN'],'PLANNED'
FROM control_plane.agents agent
WHERE agent.organization_id=@organization_id::uuid AND agent.project_id=@project_id::uuid
  AND agent.ref=@agent_ref AND agent.state='READY' AND agent.enabled
RETURNING id::text
