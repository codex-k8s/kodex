UPDATE control_plane.schedules
SET next_run_at = next_run_at - interval '7 days',
    updated_at = clock_timestamp()
WHERE ref = $1
