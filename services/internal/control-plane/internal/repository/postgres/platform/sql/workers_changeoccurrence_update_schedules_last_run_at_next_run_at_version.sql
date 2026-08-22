-- name: workers_changeoccurrence_update_schedules_last_run_at_next_run_at_version :exec
UPDATE control_plane.schedules SET last_run_at=clock_timestamp(),next_run_at=clock_timestamp()+interval '1 day',version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid
