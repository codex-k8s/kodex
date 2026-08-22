-- name: runtime_callback_insert_continuation_node :one
INSERT INTO control_plane.run_nodes(
    ref, organization_id, root_run_id, run_id, parent_node_id, type, state,
    display_name, role, agent_id, turn_id, workflow_step_key,
    human_gate_after, attempt, input_summary, next_actions
) VALUES (
    @node_ref, @organization_id::uuid, @root_run_id::uuid, @parent_run_id::uuid,
    @parent_node_id::uuid, 'AGENT_EXECUTION', 'QUEUED', @display_name, @role,
    @agent_id::uuid, @turn_id::uuid, @workflow_step_key,
    @human_gate_after, @attempt, @input_summary, ARRAY['OPEN','CANCEL']
)
RETURNING id::text
