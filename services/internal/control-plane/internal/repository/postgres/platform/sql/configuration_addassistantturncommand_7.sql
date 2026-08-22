-- name: platform__configuration_addassistantturncommand_7 :exec
UPDATE control_plane.runs SET root_run_id=id,started_at=clock_timestamp() WHERE id=$1::uuid
