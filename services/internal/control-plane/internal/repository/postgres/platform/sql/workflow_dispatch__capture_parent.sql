-- name: workflow_dispatch__capture_parent :one
SELECT node.agent_id::text,node.attempt,node.display_name,node.role,run.session_id::text,agent.ref,run.task
FROM control_plane.run_nodes node
JOIN control_plane.runs run ON run.id=node.run_id AND run.organization_id=node.organization_id
JOIN control_plane.runs root ON root.id=node.root_run_id AND root.organization_id=node.organization_id
JOIN control_plane.projects project ON project.id=root.project_id AND project.organization_id=root.organization_id
JOIN control_plane.agents agent ON agent.id=node.agent_id AND agent.organization_id=node.organization_id AND agent.project_id=project.id
JOIN control_plane.subjects actor ON actor.id=root.initiated_by AND actor.organization_id=root.organization_id AND actor.active
JOIN control_plane.catalog_access_targets target ON target.organization_id=node.organization_id AND target.kind='AGENT' AND target.id=agent.id
WHERE node.organization_id=@organization_id::uuid AND node.id=@node_id::uuid AND node.root_run_id=@root_run_id::uuid
    AND node.run_id=@run_id::uuid AND node.state='SUCCEEDED' AND node.workflow_business_attempt>0
    AND root.state IN ('RUNNING','WAITING_HUMAN') AND project.lifecycle='ACTIVE' AND agent.enabled AND agent.state='READY'
    AND control_plane.catalog_resource_visible(node.organization_id,actor.id,'agent.launch','AGENT',target.id,
        target.project_id,target.owner_id,target.related_ids,statement_timestamp(),false);
