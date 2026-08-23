-- name: runtime_claimexecution_update_run_nodes_state_started_at_version :exec
UPDATE control_plane.run_nodes SET state='RUNNING',started_at=COALESCE(started_at,clock_timestamp()),version=version+1 WHERE id=$1::uuid
