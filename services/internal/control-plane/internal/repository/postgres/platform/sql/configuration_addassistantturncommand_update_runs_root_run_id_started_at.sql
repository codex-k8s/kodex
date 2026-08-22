-- name: configuration_addassistantturncommand_update_runs_root_run_id_started_at :exec
UPDATE control_plane.runs SET root_run_id=id,started_at=clock_timestamp() WHERE id=$1::uuid
