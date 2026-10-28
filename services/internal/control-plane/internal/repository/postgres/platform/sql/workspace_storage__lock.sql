-- name: workspace_storage__lock :one
SELECT workspace.ref,workspace.generation,workspace.state
FROM control_plane.session_workspace_storage workspace
JOIN control_plane.sessions session ON session.id=workspace.session_id AND session.organization_id=workspace.organization_id
    AND session.project_id=workspace.project_id
WHERE workspace.organization_id=@organization_id::uuid AND workspace.session_id=@session_id::uuid
    AND workspace.project_id=@project_id::uuid AND workspace.agent_id=@agent_id::uuid
FOR UPDATE OF workspace,session NOWAIT;
