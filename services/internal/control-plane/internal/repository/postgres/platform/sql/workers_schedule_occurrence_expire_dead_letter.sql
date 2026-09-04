-- name: workers_schedule_occurrence_expire_dead_letter :many
WITH candidates AS (
    SELECT id FROM control_plane.schedule_occurrences
    WHERE organization_id = $1::uuid AND state = 'CLAIMED'
      AND attempt >= 3 AND lease_expires_at <= clock_timestamp()
    ORDER BY lease_expires_at, id
    FOR UPDATE SKIP LOCKED LIMIT $2
), expired AS (
    UPDATE control_plane.schedule_occurrence_attempts attempt
    SET state = 'EXPIRED',
        safe_error_code = 'SCHEDULE_LEASE_EXPIRED',
        completed_at = clock_timestamp()
    FROM control_plane.schedule_occurrences occurrence
    WHERE occurrence.organization_id = $1::uuid
      AND attempt.occurrence_id = occurrence.id
      AND attempt.attempt = occurrence.attempt
      AND attempt.generation = occurrence.generation
      AND attempt.state = 'CLAIMED'
      AND occurrence.state = 'CLAIMED'
      AND occurrence.attempt >= 3
      AND occurrence.lease_expires_at <= clock_timestamp()
      AND occurrence.id IN (SELECT id FROM candidates)
    RETURNING attempt.occurrence_id
)
UPDATE control_plane.schedule_occurrences occurrence
SET state = 'DEAD_LETTER',
    safe_error_code = 'SCHEDULE_LEASE_EXPIRED',
    lease_ref = NULL,
    fence_digest = NULL,
    workload_instance = NULL,
    lease_expires_at = NULL,
    completed_at = clock_timestamp(),
    dead_lettered_at = clock_timestamp(),
    version = version + 1,
    updated_at = clock_timestamp()
WHERE occurrence.id IN (SELECT occurrence_id FROM expired)
RETURNING occurrence.id::text
