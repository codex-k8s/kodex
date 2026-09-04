-- name: workers_schedule_occurrence_fail :one
UPDATE control_plane.schedule_occurrences
SET state = CASE WHEN $2::boolean AND attempt < 3 THEN 'RETRY_WAIT' ELSE 'DEAD_LETTER' END,
    safe_error_code = $3,
    lease_ref = NULL,
    fence_digest = NULL,
    workload_instance = NULL,
    lease_expires_at = NULL,
    completed_at = CASE WHEN $2::boolean AND attempt < 3 THEN NULL ELSE clock_timestamp() END,
    dead_lettered_at = CASE WHEN $2::boolean AND attempt < 3 THEN NULL ELSE clock_timestamp() END,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE id = $1::uuid AND state = 'CLAIMED'
RETURNING state, attempt
