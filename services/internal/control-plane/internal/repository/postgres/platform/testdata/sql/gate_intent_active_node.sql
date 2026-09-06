-- name: gate_intent_active_node :exec
UPDATE control_plane.run_nodes SET state='RUNNING'
WHERE root_run_id=(SELECT root_run_id FROM control_plane.owner_gates WHERE organization_id=$1::uuid AND ref=$2)
  AND type='AGENT_EXECUTION' AND materialization_state='MATERIALIZED';
