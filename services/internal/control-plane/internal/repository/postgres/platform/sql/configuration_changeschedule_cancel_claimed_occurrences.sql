-- name: configuration_changeschedule_cancel_claimed_occurrences :exec
UPDATE control_plane.schedule_occurrences
SET state = 'CANCELLED',
    lease_ref = NULL,
    fence_digest = NULL,
    workload_instance = NULL,
    lease_expires_at = NULL,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE schedule_id = $1::uuid
  AND state = 'CLAIMED'
