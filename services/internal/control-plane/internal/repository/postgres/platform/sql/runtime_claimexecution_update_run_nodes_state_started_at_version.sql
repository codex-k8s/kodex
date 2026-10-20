-- name: runtime_claimexecution_update_run_nodes_state_started_at_version :exec
WITH started_turn AS (
    UPDATE control_plane.session_turns turn
    SET state = 'RUNNING',
        started_at = COALESCE(turn.started_at, clock_timestamp())
    FROM control_plane.run_nodes node
    WHERE node.id = $1::uuid
      AND node.turn_id = turn.id
      AND node.organization_id = turn.organization_id
      AND turn.state = 'QUEUED'
)
UPDATE control_plane.run_nodes
SET state = 'RUNNING',
    started_at = COALESCE(started_at, clock_timestamp()),
    version = version + 1
WHERE id = $1::uuid;
