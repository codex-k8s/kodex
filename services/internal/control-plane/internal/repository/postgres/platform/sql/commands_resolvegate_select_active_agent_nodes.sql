-- name: commands_resolvegate_select_active_agent_nodes :one
SELECT count(*)
FROM control_plane.run_nodes
WHERE root_run_id=$1::uuid
  AND type='AGENT_EXECUTION'
  AND state IN ('QUEUED','RUNNING','WAITING')
