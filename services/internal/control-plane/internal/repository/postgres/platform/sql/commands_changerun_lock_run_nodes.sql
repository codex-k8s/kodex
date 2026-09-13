-- name: commands_changerun_lock_run_nodes :many
SELECT id::text
FROM control_plane.run_nodes
WHERE root_run_id=$1::uuid
  AND state IN ('PLANNED', 'QUEUED', 'RUNNING', 'WAITING')
ORDER BY id
FOR UPDATE
