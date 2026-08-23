-- name: commands_launchrun_select_sessions_organization_id_project_id_ref :one
SELECT session.id::text
FROM control_plane.sessions AS session
WHERE session.organization_id = $1::uuid
  AND session.project_id = $2::uuid
  AND session.ref = $3
  AND session.target_type = $4
  AND session.target_ref = $5
  AND session.state = 'ACTIVE'
  AND NOT EXISTS (
    SELECT 1
    FROM control_plane.session_turns AS active_turn
    WHERE active_turn.session_id = session.id
      AND active_turn.state IN ('QUEUED', 'RUNNING')
  )
FOR UPDATE
