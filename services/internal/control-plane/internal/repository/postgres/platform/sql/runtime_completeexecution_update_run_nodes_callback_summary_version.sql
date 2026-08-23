-- name: runtime_completeexecution_update_run_nodes_callback_summary_version :exec
UPDATE control_plane.run_nodes SET callback_summary=$2,version=version+1 WHERE id=$1::uuid
