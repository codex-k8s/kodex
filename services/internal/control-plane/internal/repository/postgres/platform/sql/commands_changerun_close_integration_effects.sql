-- name: commands_changerun_close_integration_effects :many
WITH graph AS MATERIALIZED (
    SELECT id FROM control_plane.runs
    WHERE organization_id = $1::uuid AND root_run_id = $2::uuid
), closed_repository_bundles AS (
    UPDATE control_plane.repository_bundle_publications
    SET state=CASE WHEN continuation_state='BLOCKED' THEN state WHEN effect_started_at IS NULL THEN 'CANCELLED' ELSE 'UNKNOWN_OUTCOME' END,
        failure_code=CASE WHEN continuation_state='BLOCKED' THEN failure_code ELSE 'AUTHORITY_CHANGED' END,
        continuation_state=CASE WHEN continuation_state='BLOCKED' THEN 'CANCELLED' ELSE continuation_state END,
        continuation_code=CASE WHEN continuation_state='BLOCKED' THEN '' ELSE continuation_code END,
        claimant='',fence='',lease_expires_at=NULL,invocation_lease_ref='',version=version+1,
        completed_at=CASE WHEN continuation_state='BLOCKED' THEN completed_at WHEN effect_started_at IS NULL THEN clock_timestamp() ELSE NULL END,
        updated_at=clock_timestamp()
    WHERE organization_id = $1::uuid AND origin_run_id IN (SELECT id FROM graph)
      AND (state IN ('WAITING_APPROVAL','QUEUED','CLAIMED','EFFECT_STARTED','UNKNOWN_OUTCOME') OR continuation_state='BLOCKED')
), closed_invocations AS (
    UPDATE control_plane.integration_invocations
    SET state = CASE WHEN operation='github.repository.bundle.publish' AND EXISTS (SELECT 1 FROM control_plane.repository_bundle_publications publication WHERE publication.integration_invocation_id=integration_invocations.id AND publication.effect_started_at IS NULL) THEN 'CANCELLED' WHEN state = 'RUNNING' AND risk <> 'READ' THEN 'UNKNOWN_OUTCOME' ELSE 'CANCELLED' END,
        lease_ref = NULL, effect_fence_digest = NULL, workload_instance = NULL, lease_expires_at = NULL,
        safe_error_code = CASE WHEN operation='github.repository.bundle.publish' AND EXISTS (SELECT 1 FROM control_plane.repository_bundle_publications publication WHERE publication.integration_invocation_id=integration_invocations.id AND publication.effect_started_at IS NULL) THEN '' WHEN state = 'RUNNING' AND risk <> 'READ' THEN 'INTEGRATION_OUTCOME_UNKNOWN' ELSE '' END,
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
