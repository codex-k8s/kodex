-- name: platform__commands_changerun_8 :exec
UPDATE control_plane.runs SET retry_of_run_id=$2::uuid,attempt=$3 WHERE id=$1::uuid
