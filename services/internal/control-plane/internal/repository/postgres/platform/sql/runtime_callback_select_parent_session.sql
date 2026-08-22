-- name: runtime_callback_select_parent_session :one
SELECT session.id::text,
       session.next_turn_number
FROM control_plane.runs parent_run
JOIN control_plane.sessions session ON session.id = parent_run.session_id
WHERE parent_run.organization_id = @organization_id::uuid
  AND parent_run.id = @parent_run_id::uuid
FOR UPDATE OF session
