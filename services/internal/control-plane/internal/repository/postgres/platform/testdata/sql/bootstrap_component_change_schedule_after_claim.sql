UPDATE control_plane.schedules
SET input = '{"task":"Changed after claim"}'::jsonb,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE ref = $1
