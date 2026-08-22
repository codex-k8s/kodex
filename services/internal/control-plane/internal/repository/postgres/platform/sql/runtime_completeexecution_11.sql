-- name: platform__runtime_completeexecution_11 :exec
UPDATE control_plane.run_nodes SET callback_summary=$2,version=version+1 WHERE id=$1::uuid
