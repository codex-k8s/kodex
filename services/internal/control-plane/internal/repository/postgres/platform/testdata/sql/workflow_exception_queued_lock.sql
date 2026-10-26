-- name: workflow_exception_queued_lock :one
SELECT node.ref FROM control_plane.run_nodes node
JOIN control_plane.runs root ON root.id=node.root_run_id AND root.organization_id=node.organization_id
JOIN control_plane.projects project ON project.id=root.project_id AND project.organization_id=root.organization_id
JOIN control_plane.agents agent ON agent.id=node.agent_id AND agent.organization_id=node.organization_id AND agent.project_id=root.project_id
WHERE root.ref=@run_ref AND project.ref=@project_ref AND node.id=@node_id::uuid AND agent.ref=@agent_ref
    AND node.state='QUEUED' AND node.workflow_business_attempt=2 AND node.workflow_step_key='a'
FOR UPDATE OF node;
