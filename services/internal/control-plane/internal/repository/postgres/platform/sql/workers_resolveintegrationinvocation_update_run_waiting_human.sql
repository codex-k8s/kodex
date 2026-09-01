-- name: workers_resolveintegrationinvocation_update_run_waiting_human :exec
UPDATE control_plane.runs
SET state='WAITING_HUMAN',version=version+1,updated_at=clock_timestamp()
WHERE root_run_id=$1::uuid AND state='RUNNING'
