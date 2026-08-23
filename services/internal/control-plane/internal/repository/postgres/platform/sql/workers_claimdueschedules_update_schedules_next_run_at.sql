-- name: workers_claimdueschedules_update_schedules_next_run_at :exec
UPDATE control_plane.schedules
SET next_run_at = $2,
    updated_at = clock_timestamp()
WHERE id = $1::uuid
  AND next_run_at = $3
