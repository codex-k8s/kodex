-- name: workspace_claim_contention_lock :one
SELECT session.ref
FROM control_plane.sessions session
JOIN control_plane.runs run ON run.session_id=session.id AND run.organization_id=session.organization_id
WHERE run.ref=$1
FOR UPDATE OF session;
