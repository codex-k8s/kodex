-- name: workflow_exception__agent :one
SELECT agent.id::text,agent.name,agent.role_description
FROM control_plane.runs root
JOIN control_plane.projects project ON project.id=root.project_id AND project.organization_id=root.organization_id AND project.lifecycle='ACTIVE'
JOIN control_plane.subjects actor ON actor.id=root.initiated_by AND actor.organization_id=root.organization_id AND actor.active
JOIN control_plane.agents agent ON agent.organization_id=root.organization_id AND agent.project_id=root.project_id
    AND agent.ref=@agent_ref AND agent.enabled AND agent.state='READY'
JOIN control_plane.catalog_access_targets target ON target.organization_id=agent.organization_id AND target.kind='AGENT' AND target.id=agent.id
WHERE root.id=@root_run_id::uuid AND root.organization_id=@organization_id::uuid AND root.root_run_id=root.id
    AND root.state IN ('QUEUED','RUNNING','WAITING_HUMAN')
    AND control_plane.catalog_resource_visible(root.organization_id,actor.id,'agent.launch','AGENT',target.id,target.project_id,target.owner_id,target.related_ids,statement_timestamp(),false);
