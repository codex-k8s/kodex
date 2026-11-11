-- name: native_callback_audit_count :one
SELECT count(*)::int
FROM control_plane.session_turns turn
JOIN control_plane.sessions session ON session.id = turn.session_id AND session.organization_id = turn.organization_id
WHERE session.ref = $1 AND turn.content = $2 AND turn.actor_kind = 'AGENT' AND turn.state = 'COMPLETED';
