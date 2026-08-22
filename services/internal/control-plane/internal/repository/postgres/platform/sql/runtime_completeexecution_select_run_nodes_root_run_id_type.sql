-- name: runtime_completeexecution_select_run_nodes_root_run_id_type :one
SELECT count(*) FROM control_plane.run_nodes WHERE root_run_id=$1::uuid AND type='AGENT_EXECUTION' AND state IN('QUEUED','RUNNING','WAITING')
