-- name: workspace_storage__session :one
SELECT session.id::text
FROM control_plane.sessions session
JOIN control_plane.agents agent ON agent.id=@agent_id::uuid AND agent.organization_id=session.organization_id
    AND agent.project_id=session.project_id
JOIN control_plane.runs run ON run.id=@run_id::uuid AND run.session_id=session.id
    AND run.organization_id=session.organization_id AND run.project_id=session.project_id
WHERE session.id=@session_id::uuid AND session.organization_id=@organization_id::uuid
    AND session.project_id=@project_id::uuid
    AND control_plane.workspace_session_scope_matches(session.id,agent.id,
        CASE WHEN session.target_type='WORKFLOW' THEN run.workflow_version_id END)
FOR UPDATE OF session NOWAIT;
