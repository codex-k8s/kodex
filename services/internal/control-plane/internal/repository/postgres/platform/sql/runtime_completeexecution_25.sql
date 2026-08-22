-- name: platform__runtime_completeexecution_25 :exec
UPDATE control_plane.schedules SET last_run_at=clock_timestamp(),version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid
