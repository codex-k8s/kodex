-- name: platform__runtime_completeexecution_21 :one
SELECT count(*) FROM control_plane.run_nodes WHERE root_run_id=$1::uuid AND type='AGENT_EXECUTION' AND state IN('QUEUED','RUNNING','WAITING')
