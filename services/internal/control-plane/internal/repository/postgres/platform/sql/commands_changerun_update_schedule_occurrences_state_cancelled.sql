-- name: commands_changerun_update_schedule_occurrences_state_cancelled :exec
UPDATE control_plane.schedule_occurrences
SET state = 'CANCELLED',
    lease_ref = NULL,
    fence_digest = NULL,
    workload_instance = NULL,
    lease_expires_at = NULL,
    completed_at = clock_timestamp(),
    version = version + 1,
    updated_at = clock_timestamp()
WHERE run_id = $1::uuid
  AND state = 'MATERIALIZED'
