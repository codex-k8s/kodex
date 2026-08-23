-- name: interaction_cancel_pending_gate_deliveries :exec
UPDATE control_plane.interaction_deliveries
SET state = 'CANCELLED',
    lease_ref = NULL,
    fence_digest = NULL,
    workload_instance = NULL,
    lease_expires_at = NULL,
    version = version + 1,
    updated_at = clock_timestamp(),
    completed_at = clock_timestamp()
WHERE gate_id = @gate_id::uuid
  AND state IN ('DUE', 'FAILED', 'CLAIMED')
