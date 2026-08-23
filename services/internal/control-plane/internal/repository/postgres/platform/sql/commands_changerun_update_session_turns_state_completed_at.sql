-- name: commands_changerun_update_session_turns_state_completed_at :exec
UPDATE control_plane.session_turns
SET state = 'CANCELLED',
    completed_at = clock_timestamp()
WHERE run_id IN (
    SELECT id
    FROM control_plane.runs
    WHERE root_run_id = $1::uuid
)
  AND state IN ('QUEUED', 'RUNNING')
