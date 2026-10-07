-- name: workflow_launch__resolve_origin :one
SELECT root.id::text,run.id::text,node.id::text,session.id::text,turn.id::text,revision.id::text,
       project.id::text,project.ref,actor.id::text,actor.ref,actor.display_name,'MEMBER',
       organization.ref,agent.ref,agent.capabilities,node.attempt,revision.input_digest,revision.revision_digest
FROM control_plane.runtime_leases lease
JOIN control_plane.runtime_revisions revision ON revision.id=lease.runtime_revision_id AND revision.organization_id=lease.organization_id
JOIN control_plane.run_nodes node ON node.id=lease.node_id AND node.organization_id=lease.organization_id AND node.run_id=lease.run_id
JOIN control_plane.runs run ON run.id=node.run_id AND run.organization_id=lease.organization_id
JOIN control_plane.runs root ON root.id=run.root_run_id AND root.organization_id=lease.organization_id
JOIN control_plane.sessions session ON session.id=run.session_id AND session.organization_id=lease.organization_id
JOIN control_plane.session_turns turn ON turn.id=node.turn_id AND turn.session_id=session.id AND turn.run_id=run.id
JOIN control_plane.projects project ON project.id=run.project_id AND project.organization_id=lease.organization_id AND project.lifecycle='ACTIVE'
JOIN control_plane.organizations organization ON organization.id=lease.organization_id
JOIN control_plane.agents agent ON agent.id=node.agent_id AND agent.organization_id=lease.organization_id AND agent.project_id=project.id
JOIN control_plane.subjects actor ON actor.id=root.initiated_by AND actor.organization_id=lease.organization_id AND actor.active AND actor.kind='USER'
WHERE lease.organization_id=@organization_id::uuid AND lease.ref=@lease_ref
 AND lease.fence_digest=@fence_digest AND lease.generation=@generation AND lease.state='CLAIMED' AND lease.expires_at>clock_timestamp()
 AND root.state IN ('RUNNING','WAITING_HUMAN') AND run.state='RUNNING' AND node.state='RUNNING' AND turn.state IN ('QUEUED','RUNNING')
 AND agent.enabled AND agent.state IN ('READY','RUNNING') AND agent.system_key IS NULL
 AND revision.run_id=run.id AND revision.node_id=node.id AND revision.root_run_id=root.id
 AND revision.session_id=session.id AND revision.turn_id=turn.id AND revision.agent_id=agent.id AND revision.project_id=project.id
 AND revision.generation=lease.generation AND revision.attempt=node.attempt
 AND 'platform.run.launch'=ANY(revision.capabilities) AND 'platform.run.launch'=ANY(agent.capabilities)
 AND revision.safe_snapshot->>'assistantScope'='NONE'
FOR UPDATE OF node,lease,root,actor;
