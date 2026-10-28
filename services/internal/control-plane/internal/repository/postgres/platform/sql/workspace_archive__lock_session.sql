-- name: workspace_archive__lock_session :one
SELECT s.id::text FROM control_plane.sessions s
WHERE s.id=@session_id::uuid AND s.organization_id=@organization_id::uuid
FOR UPDATE OF s NOWAIT;
