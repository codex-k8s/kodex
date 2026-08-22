-- name: platform__commands_resolvegate_update_root_node_state :one
UPDATE control_plane.run_nodes
SET state = $2,
    finished_at = clock_timestamp(),
    next_actions = ARRAY['OPEN'],
    version = version + 1
WHERE root_run_id = $1::uuid
  AND type = 'ROOT_PROCESS'
RETURNING ref
