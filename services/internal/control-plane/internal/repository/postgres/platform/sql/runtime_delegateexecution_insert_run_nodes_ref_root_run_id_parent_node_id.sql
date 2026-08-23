-- name: runtime_delegateexecution_insert_run_nodes_ref_root_run_id_parent_node_id :one
INSERT INTO control_plane.run_nodes(
    ref, organization_id, root_run_id, run_id, parent_node_id, type, state,
    display_name, role, agent_id, turn_id, workflow_step_key, input_summary,
    next_actions
) VALUES (
    @node_ref, @organization_id::uuid, @root_run_id::uuid, @run_id::uuid,
    @parent_node_id::uuid, 'AGENT_EXECUTION', 'QUEUED', @display_name, @role,
    @agent_id::uuid, @turn_id::uuid, @workflow_step_key, @input_summary,
    ARRAY['OPEN','CANCEL']
)
RETURNING id::text
