-- name: commands_changerun_close_integration_effects :many
WITH graph AS MATERIALIZED (
    SELECT id FROM control_plane.runs
    WHERE organization_id = $1::uuid AND root_run_id = $2::uuid
), closed_invocations AS (
    UPDATE control_plane.integration_invocations
    SET state = CASE WHEN state = 'RUNNING' AND risk <> 'READ' THEN 'UNKNOWN_OUTCOME' ELSE 'CANCELLED' END,
        lease_ref = NULL, effect_fence_digest = NULL, workload_instance = NULL, lease_expires_at = NULL,
        safe_error_code = CASE WHEN state = 'RUNNING' AND risk <> 'READ' THEN 'INTEGRATION_OUTCOME_UNKNOWN' ELSE '' END,
        version = version + 1, updated_at = clock_timestamp()
    WHERE organization_id = $1::uuid AND run_id IN (SELECT id FROM graph)
      AND state IN ('WAITING_APPROVAL', 'READY', 'RUNNING')
    RETURNING ref, node_id, state
), closed_gate_deliveries AS (
    UPDATE control_plane.interaction_deliveries delivery
    SET state = CASE WHEN delivery.state = 'CLAIMED' THEN 'UNKNOWN_OUTCOME' ELSE 'CANCELLED' END,
        safe_error_code = CASE WHEN delivery.state = 'CLAIMED' THEN 'INTERACTION_OUTCOME_UNKNOWN' ELSE delivery.safe_error_code END,
        lease_ref = NULL, fence_digest = NULL, workload_instance = NULL, lease_expires_at = NULL,
        version = version + 1, updated_at = clock_timestamp(), completed_at = clock_timestamp()
    WHERE delivery.gate_id IN (
        SELECT id FROM control_plane.owner_gates
        WHERE organization_id = $1::uuid AND root_run_id = $2::uuid AND state = 'OPEN'
    ) AND delivery.state IN ('WAITING_APPROVAL', 'DUE', 'FAILED', 'CLAIMED')
)
SELECT invocation.ref, node.ref AS node_ref, invocation.state
FROM closed_invocations invocation
JOIN control_plane.run_nodes node ON node.id = invocation.node_id
ORDER BY invocation.ref
