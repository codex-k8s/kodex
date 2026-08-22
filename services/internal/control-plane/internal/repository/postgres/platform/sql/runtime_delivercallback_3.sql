-- name: platform__runtime_delivercallback_3 :exec
UPDATE control_plane.run_nodes SET callback_summary=$2,version=version+1 WHERE id=$1::uuid
