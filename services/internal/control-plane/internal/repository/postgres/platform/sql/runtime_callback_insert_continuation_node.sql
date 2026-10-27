-- name: runtime_callback_insert_continuation_node :one
INSERT INTO control_plane.run_nodes(
    ref, organization_id, root_run_id, run_id, parent_node_id, type, state,
    display_name, role, agent_id, turn_id, workflow_step_key,
    human_gate_after, attempt, input_summary, next_actions,
    workflow_step_origin_node_id,workflow_business_attempt,workflow_inputs,workflow_rework_gate_id,owner_rework_gate_id,owner_rework_origin_node_id
) SELECT
    @node_ref, @organization_id::uuid, @root_run_id::uuid, @parent_run_id::uuid,
    @parent_node_id::uuid, 'AGENT_EXECUTION', 'QUEUED', @display_name, @role,
    @agent_id::uuid, @turn_id::uuid,
    CASE WHEN parent.workflow_business_attempt>0 OR parent.owner_rework_gate_id IS NOT NULL THEN parent.workflow_step_key ELSE @workflow_step_key END,
    CASE WHEN parent.owner_rework_gate_id IS NOT NULL THEN parent.human_gate_after ELSE @human_gate_after END,
    @attempt, @input_summary, ARRAY['OPEN','CANCEL'],
    CASE WHEN parent.workflow_business_attempt>0 THEN COALESCE(parent.workflow_step_origin_node_id,parent.id) END,
    parent.workflow_business_attempt,parent.workflow_inputs,parent.workflow_rework_gate_id,parent.owner_rework_gate_id,
    CASE WHEN parent.owner_rework_gate_id IS NOT NULL THEN COALESCE(parent.owner_rework_origin_node_id,parent.id) END
FROM control_plane.run_nodes parent
WHERE parent.id=@parent_node_id::uuid AND parent.organization_id=@organization_id::uuid
    AND parent.root_run_id=@root_run_id::uuid AND parent.run_id=@parent_run_id::uuid
    AND parent.agent_id=@agent_id::uuid
    AND (parent.workflow_business_attempt=0 OR (
        parent.state='SUCCEEDED' AND parent.workflow_captured_artifacts IS NOT NULL
        AND @attempt::integer=parent.attempt+1
        AND NOT EXISTS(SELECT 1 FROM control_plane.run_edges continuation
            WHERE continuation.organization_id=parent.organization_id AND continuation.root_run_id=parent.root_run_id
                AND continuation.source_node_id=parent.id AND continuation.type='CONTINUES')
        AND (SELECT count(*) FROM control_plane.run_edges continuation
            WHERE continuation.organization_id=parent.organization_id AND continuation.root_run_id=parent.root_run_id
                AND continuation.type='CONTINUES')<64
    ))
RETURNING control_plane.run_nodes.id::text
