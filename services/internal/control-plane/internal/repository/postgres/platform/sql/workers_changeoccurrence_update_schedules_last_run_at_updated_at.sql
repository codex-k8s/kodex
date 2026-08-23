-- name: workers_changeoccurrence_update_schedules_last_run_at_updated_at :exec
UPDATE control_plane.schedules
SET last_run_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = $1::uuid
