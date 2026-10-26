-- name: workflow_dispatch__materialize :one
UPDATE control_plane.run_nodes anchor
SET run_id=@run_id::uuid,turn_id=@turn_id::uuid,state='QUEUED',materialization_state='MATERIALIZED',
    workflow_business_attempt=1,workflow_inputs=@inputs::jsonb,input_summary=@input_summary,
    next_actions=ARRAY['OPEN','CANCEL'],version=version+1
WHERE anchor.id=@node_id::uuid AND anchor.organization_id=@organization_id::uuid
    AND anchor.root_run_id=@root_run_id::uuid AND anchor.workflow_step_key=@step_key
    AND anchor.agent_id=@agent_id::uuid AND anchor.workflow_step_origin_node_id IS NULL
    AND anchor.state='PLANNED' AND anchor.materialization_state='PLANNED'
    AND anchor.workflow_business_attempt=0 AND anchor.turn_id IS NULL
RETURNING anchor.ref;
