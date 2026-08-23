-- name: workers_claimdueschedules_reclaim_expired_occurrence :one
UPDATE control_plane.schedule_occurrences
SET lease_ref = $2,
    fence_digest = $3,
    workload_instance = $4,
    lease_expires_at = $5,
    generation = generation + 1,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE id = $1::uuid
  AND state = 'CLAIMED'
  AND generation = $6
  AND lease_expires_at <= clock_timestamp()
RETURNING generation
