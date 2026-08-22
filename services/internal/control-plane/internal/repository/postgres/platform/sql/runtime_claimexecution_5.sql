-- name: platform__runtime_claimexecution_5 :exec
UPDATE control_plane.run_nodes SET state='RUNNING',started_at=COALESCE(started_at,clock_timestamp()),version=version+1 WHERE id=$1::uuid
