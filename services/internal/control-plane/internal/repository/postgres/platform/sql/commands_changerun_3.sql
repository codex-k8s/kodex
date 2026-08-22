-- name: platform__commands_changerun_3 :exec
UPDATE control_plane.run_nodes SET state='CANCELLED',next_actions=ARRAY['OPEN','RETRY'],finished_at=clock_timestamp(),version=version+1 WHERE root_run_id=$1::uuid AND state IN ('QUEUED','RUNNING','WAITING')
