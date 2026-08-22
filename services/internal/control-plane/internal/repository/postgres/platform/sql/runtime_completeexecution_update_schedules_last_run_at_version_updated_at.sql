-- name: platform__runtime_completeexecution_update_schedules_last_run_at_version_updated_at :exec
UPDATE control_plane.schedules SET last_run_at=clock_timestamp(),version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid
