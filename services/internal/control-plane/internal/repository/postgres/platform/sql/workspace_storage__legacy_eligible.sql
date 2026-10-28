-- name: workspace_storage__legacy_eligible :one
SELECT NOT EXISTS (
    SELECT 1 FROM control_plane.session_workspace_storage workspace
    WHERE workspace.session_id=session.id
)
FROM control_plane.sessions session
WHERE session.id=@session_id::uuid AND session.organization_id=@organization_id::uuid
    AND session.project_id=@project_id::uuid AND session.state='ACTIVE'
FOR UPDATE OF session NOWAIT;
