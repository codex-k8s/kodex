-- name: runtime_claimexecution_update_run_nodes_state_started_at_version :exec
WITH cleared_queue AS (
    UPDATE control_plane.runs root
    SET state = 'RUNNING', started_at = COALESCE(root.started_at, clock_timestamp()),
        presentation_metadata = root.presentation_metadata - 'runtimeQueueReason', version = version + 1
    WHERE root.id = (SELECT node.root_run_id FROM control_plane.run_nodes node WHERE node.id = $1::uuid)
      AND root.state IN ('QUEUED', 'RUNNING')
      AND (root.presentation_metadata ? 'runtimeQueueReason' OR root.state = 'QUEUED')
), started_turn AS (
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
    progress_summary = 'i18n:WORKLOAD_STARTING',
    started_at = COALESCE(started_at, clock_timestamp()),
    version = version + 1
WHERE id = $1::uuid;
