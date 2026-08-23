UPDATE control_plane.schedules
SET next_run_at = clock_timestamp() - interval '1 minute',
    updated_at = clock_timestamp()
WHERE ref = $1
