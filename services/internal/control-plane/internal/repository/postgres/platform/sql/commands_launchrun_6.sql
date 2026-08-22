-- name: platform__commands_launchrun_6 :exec
UPDATE control_plane.runs SET root_run_id=id WHERE id=$1::uuid
