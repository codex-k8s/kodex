-- name: runtime_reportprogress_update_run_nodes_progress_summary_version :exec
UPDATE control_plane.run_nodes SET progress_summary=$2,version=version+1 WHERE id=$1::uuid
