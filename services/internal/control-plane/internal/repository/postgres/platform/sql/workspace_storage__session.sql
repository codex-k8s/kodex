-- name: workspace_storage__session :one
SELECT session.id::text
FROM control_plane.sessions session
JOIN control_plane.agents agent ON agent.id=@agent_id::uuid AND agent.organization_id=session.organization_id
    AND agent.project_id=session.project_id AND agent.ref=session.target_ref
WHERE session.id=@session_id::uuid AND session.organization_id=@organization_id::uuid
    AND session.project_id=@project_id::uuid AND session.target_type='AGENT'
FOR UPDATE OF session NOWAIT;
