-- name: runtime_claim__fail_graph :many
WITH graph AS MATERIALIZED (
    SELECT id FROM control_plane.runs
    WHERE organization_id = @organization_id::uuid AND root_run_id = @root_run_id::uuid
    ORDER BY id FOR UPDATE
), failed_runs AS (
    UPDATE control_plane.runs SET state = 'FAILED', safe_error_code = 'RUNTIME_INPUT_INVALID',
        safe_error_message = '', finished_at = clock_timestamp(), updated_at = clock_timestamp(), version = version + 1
    WHERE id IN (SELECT id FROM graph) AND state IN ('QUEUED', 'RUNNING', 'WAITING_HUMAN', 'CANCELLING')
    RETURNING ref
), closed_nodes AS (
    UPDATE control_plane.run_nodes
    SET state = CASE WHEN id = @failed_node_id::uuid OR type = 'ROOT_PROCESS' THEN 'FAILED' ELSE 'CANCELLED' END,
        safe_error_code = 'RUNTIME_INPUT_INVALID', safe_error_message = '', next_actions = '{}',
        finished_at = clock_timestamp(), version = version + 1
    WHERE run_id IN (SELECT id FROM graph) AND state IN ('PLANNED', 'QUEUED', 'RUNNING', 'WAITING')
    RETURNING ref, state
), closed_leases AS (
    UPDATE control_plane.runtime_leases SET state = 'CANCELLED', updated_at = clock_timestamp()
    WHERE run_id IN (SELECT id FROM graph) AND state = 'CLAIMED'
), closed_turns AS (
    UPDATE control_plane.session_turns SET state = 'FAILED', completed_at = clock_timestamp()
    WHERE run_id IN (SELECT id FROM graph) AND state IN ('QUEUED', 'RUNNING')
), closed_occurrences AS (
    UPDATE control_plane.schedule_occurrences SET state = 'FAILED', lease_ref = NULL,
        fence_digest = NULL, workload_instance = NULL, lease_expires_at = NULL,
        version = version + 1, updated_at = clock_timestamp()
    WHERE run_id IN (SELECT id FROM graph) AND state IN ('DUE', 'CLAIMED', 'MATERIALIZED')
), closed_invocations AS (
    UPDATE control_plane.integration_invocations
    SET state = CASE WHEN state = 'RUNNING' AND risk <> 'READ' THEN 'UNKNOWN_OUTCOME' ELSE 'CANCELLED' END, lease_ref = NULL,
        effect_fence_digest = NULL, workload_instance = NULL, lease_expires_at = NULL,
        safe_error_code = CASE WHEN state = 'RUNNING' AND risk <> 'READ' THEN 'INTEGRATION_OUTCOME_UNKNOWN' ELSE 'RUNTIME_INPUT_INVALID' END,
        version = version + 1, updated_at = clock_timestamp()
    WHERE run_id IN (SELECT id FROM graph) AND state IN ('WAITING_APPROVAL', 'READY', 'RUNNING')
), closed_gates AS (
    UPDATE control_plane.owner_gates gate SET state = 'CANCELLED', decision = 'CANCEL',
        decision_comment = @summary::text, resolved_by = @actor_id::uuid,
        resolved_at = clock_timestamp(), version = version + 1
    WHERE organization_id = @organization_id::uuid AND root_run_id = @root_run_id::uuid AND state = 'OPEN'
    RETURNING gate.id, gate.ref, gate.node_id
), closed_gate_deliveries AS (
    UPDATE control_plane.interaction_deliveries
    SET state = CASE WHEN state = 'CLAIMED' THEN 'UNKNOWN_OUTCOME' ELSE 'CANCELLED' END,
        safe_error_code = CASE WHEN state = 'CLAIMED' THEN 'INTERACTION_OUTCOME_UNKNOWN' ELSE safe_error_code END,
        lease_ref = NULL, fence_digest = NULL, workload_instance = NULL, lease_expires_at = NULL,
        version = version + 1, updated_at = clock_timestamp(), completed_at = clock_timestamp()
    WHERE gate_id IN (SELECT id FROM closed_gates) AND state IN ('WAITING_APPROVAL', 'DUE', 'FAILED', 'CLAIMED')
)
SELECT 'RUN'::text AS kind, ref, ''::text AS node_ref, 'FAILED'::text AS state FROM failed_runs
UNION ALL
SELECT 'NODE', ref, ref, state FROM closed_nodes
UNION ALL
SELECT 'GATE', gate.ref, node.ref, 'CANCELLED' FROM closed_gates gate
JOIN control_plane.run_nodes node ON node.id = gate.node_id
ORDER BY kind, ref
