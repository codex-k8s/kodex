-- name: workspace_storage__lock :one
SELECT workspace.ref,workspace.generation,workspace.state,
       workspace.workflow_version_id IS NOT DISTINCT FROM
           CASE WHEN session.target_type='WORKFLOW' THEN run.workflow_version_id END
FROM control_plane.session_workspace_storage workspace
JOIN control_plane.sessions session ON session.id=workspace.session_id AND session.organization_id=workspace.organization_id
    AND session.project_id=workspace.project_id
JOIN control_plane.runs run ON run.id=@run_id::uuid AND run.session_id=session.id
    AND run.organization_id=session.organization_id AND run.project_id=session.project_id
WHERE workspace.organization_id=@organization_id::uuid AND workspace.session_id=@session_id::uuid
    AND workspace.project_id=@project_id::uuid AND workspace.agent_id=@agent_id::uuid
FOR UPDATE OF workspace,session NOWAIT;
