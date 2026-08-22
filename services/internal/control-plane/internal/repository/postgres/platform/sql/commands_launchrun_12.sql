-- name: platform__commands_launchrun_12 :exec
UPDATE control_plane.runs SET state='RUNNING',started_at=clock_timestamp(),version=version+1 WHERE id=$1::uuid
