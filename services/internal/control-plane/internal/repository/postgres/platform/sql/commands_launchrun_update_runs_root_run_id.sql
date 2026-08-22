-- name: platform__commands_launchrun_update_runs_root_run_id :exec
UPDATE control_plane.runs SET root_run_id=id WHERE id=$1::uuid
