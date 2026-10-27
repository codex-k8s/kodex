-- name: workspace_storage__create :one
INSERT INTO control_plane.session_workspace_storage(ref,organization_id,project_id,session_id,agent_id,state)
SELECT @workspace_ref,session.organization_id,session.project_id,session.id,agent.id,'EMPTY'
FROM control_plane.sessions session
JOIN control_plane.agents agent ON agent.id=@agent_id::uuid AND agent.organization_id=session.organization_id
    AND agent.project_id=session.project_id AND agent.ref=session.target_ref
WHERE session.organization_id=@organization_id::uuid AND session.project_id=@project_id::uuid
    AND session.id=@session_id::uuid AND session.target_type='AGENT' AND session.state='ACTIVE'
    AND NOT EXISTS(SELECT 1 FROM control_plane.session_archive_tasks task
        WHERE task.session_id=session.id AND task.state IN ('READY','CLAIMED') AND task.kind IN ('RESTORE','DELETE_PVC'))
RETURNING ref,generation,state;
