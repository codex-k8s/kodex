-- name: commands_changerun_update_run_nodes_state_next_actions_finished_at :many
UPDATE control_plane.run_nodes
SET state = 'CANCELLED',
    next_actions = ARRAY['OPEN', 'RETRY'],
    finished_at = clock_timestamp(),
    version = version + 1
WHERE root_run_id = $1::uuid
  AND state IN ('PLANNED', 'QUEUED', 'RUNNING', 'WAITING')
RETURNING ref
