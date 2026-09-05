-- name: interaction_cancel_pending_gate_deliveries :exec
UPDATE control_plane.interaction_deliveries
SET state = CASE WHEN state = 'CLAIMED' THEN 'UNKNOWN_OUTCOME' ELSE 'CANCELLED' END,
    safe_error_code = CASE WHEN state = 'CLAIMED' THEN 'INTERACTION_OUTCOME_UNKNOWN' ELSE safe_error_code END,
    lease_ref = NULL,
    fence_digest = NULL,
    workload_instance = NULL,
    lease_expires_at = NULL,
    version = version + 1,
    updated_at = clock_timestamp(),
    completed_at = clock_timestamp()
WHERE gate_id = @gate_id::uuid
  AND state IN ('DUE', 'FAILED', 'CLAIMED')
