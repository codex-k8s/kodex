-- name: workspace_observation__lease :one
SELECT workspace.id::text,workspace.ref,workspace.generation,workspace.state,workspace.version,
       COALESCE(workspace.writer_lease_id::text,''),COALESCE(workspace.writer_acknowledgement,''),COALESCE(workspace.quiescence_record,''),
       lease.id::text,lease.fence_digest,lease.generation,lease.state,lease.expires_at,
       revision.ref,revision.generation,revision.revision_digest,revision.attempt,revision.safe_snapshot,
       organization.ref,project.ref,session.ref,agent.ref,run.ref,node.ref,turn.ref,lease.input_digest,
       project.id::text,
       (root.state IN ('QUEUED','RUNNING','WAITING_HUMAN') AND run.state IN ('QUEUED','RUNNING')
           AND node.state IN ('QUEUED','RUNNING') AND project.lifecycle='ACTIVE' AND initiator.active
           AND agent.enabled AND agent.state IN ('READY','RUNNING')) AS active
FROM control_plane.runtime_leases lease
JOIN control_plane.runtime_revisions revision ON revision.id=lease.runtime_revision_id
    AND revision.organization_id=lease.organization_id AND revision.run_id=lease.run_id AND revision.node_id=lease.node_id
    AND revision.generation=lease.generation AND revision.input_digest=lease.input_digest
JOIN control_plane.runs run ON run.id=lease.run_id AND run.organization_id=lease.organization_id
    AND run.session_id=revision.session_id AND run.project_id=revision.project_id AND run.root_run_id=revision.root_run_id
JOIN control_plane.runs root ON root.id=run.root_run_id AND root.organization_id=lease.organization_id
    AND root.root_run_id=root.id AND root.project_id=run.project_id AND root.initiated_by=run.initiated_by
JOIN control_plane.subjects initiator ON initiator.id=root.initiated_by AND initiator.organization_id=root.organization_id
JOIN control_plane.run_nodes node ON node.id=lease.node_id AND node.organization_id=lease.organization_id
    AND node.run_id=run.id AND node.root_run_id=root.id AND node.agent_id=revision.agent_id
    AND node.turn_id=revision.turn_id AND node.attempt=revision.attempt
JOIN control_plane.session_turns turn ON turn.id=revision.turn_id AND turn.organization_id=lease.organization_id
    AND turn.run_id=run.id AND turn.session_id=run.session_id
JOIN control_plane.sessions session ON session.id=run.session_id AND session.organization_id=lease.organization_id
    AND session.project_id=run.project_id AND session.target_type='AGENT'
JOIN control_plane.agents agent ON agent.id=revision.agent_id AND agent.organization_id=lease.organization_id
    AND agent.project_id=run.project_id AND agent.ref=session.target_ref
JOIN control_plane.projects project ON project.id=run.project_id AND project.organization_id=lease.organization_id
JOIN control_plane.organizations organization ON organization.id=lease.organization_id
JOIN control_plane.session_workspace_storage workspace ON workspace.session_id=session.id
    AND workspace.organization_id=lease.organization_id AND workspace.project_id=run.project_id AND workspace.agent_id=agent.id
WHERE lease.organization_id=@organization_id::uuid AND lease.ref=@lease_ref
FOR UPDATE OF lease,workspace NOWAIT;
