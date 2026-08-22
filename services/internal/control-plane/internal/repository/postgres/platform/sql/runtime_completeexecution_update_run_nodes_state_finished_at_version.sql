-- name: runtime_completeexecution_update_run_nodes_state_finished_at_version :one
UPDATE control_plane.run_nodes
SET state = $2,
    finished_at = clock_timestamp(),
    version = version + 1
WHERE root_run_id = $1::uuid
  AND type = 'ROOT_PROCESS'
RETURNING ref
