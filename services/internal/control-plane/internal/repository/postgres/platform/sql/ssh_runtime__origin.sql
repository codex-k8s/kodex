-- name: ssh_runtime__origin :one
SELECT actor.id::text,actor.ref,coalesce(project.id::text,''),run.ref,connection.ref,connection.definition_key,invocation.definition_version,invocation.definition_digest,invocation.operation
FROM control_plane.runtime_leases lease
JOIN control_plane.runtime_revisions revision ON revision.id=lease.runtime_revision_id AND revision.organization_id=lease.organization_id
JOIN control_plane.run_nodes node ON node.id=lease.node_id AND node.organization_id=lease.organization_id
JOIN control_plane.runs run ON run.id=lease.run_id AND run.organization_id=lease.organization_id
JOIN control_plane.runs root ON root.id=run.root_run_id AND root.organization_id=lease.organization_id
JOIN control_plane.subjects actor ON actor.id=root.initiated_by AND actor.organization_id=lease.organization_id AND actor.active
JOIN control_plane.agents agent ON agent.id=node.agent_id AND agent.organization_id=lease.organization_id AND agent.enabled AND agent.state<>'ARCHIVED'
JOIN control_plane.sessions session ON session.id=revision.session_id AND session.organization_id=lease.organization_id
JOIN control_plane.session_turns turn ON turn.id=revision.turn_id AND turn.organization_id=lease.organization_id
LEFT JOIN control_plane.projects project ON project.id=run.project_id AND project.organization_id=lease.organization_id
JOIN control_plane.integration_invocations invocation ON invocation.run_id=run.id AND invocation.node_id=node.id AND invocation.organization_id=lease.organization_id
JOIN control_plane.integration_grants grant_record ON grant_record.id=invocation.grant_id AND grant_record.organization_id=lease.organization_id
JOIN control_plane.integration_connections connection ON connection.id=invocation.connection_id AND connection.organization_id=invocation.organization_id
JOIN control_plane.ssh_invocation_identity_bindings pin ON pin.invocation_id=invocation.id AND pin.organization_id=lease.organization_id
WHERE lease.organization_id=@organization_id::uuid
 AND lease.ref=@lease_ref AND lease.fence_digest=@fence_digest AND lease.generation=@generation
 AND lease.input_digest=@runtime_input_digest AND lease.state='CLAIMED' AND lease.expires_at>clock_timestamp()
 AND control_plane.runtime_execution_before_deadline(lease.organization_id,lease.run_id)
 AND root.root_run_id=root.id
 AND revision.run_id=run.id AND revision.node_id=node.id AND revision.root_run_id=root.id
 AND revision.generation=lease.generation AND revision.input_digest=lease.input_digest AND revision.agent_id=agent.id
 AND revision.project_id IS NOT DISTINCT FROM run.project_id AND revision.project_id IS NOT DISTINCT FROM root.project_id
 AND node.run_id=run.id AND node.root_run_id=root.id AND node.attempt=revision.attempt AND node.turn_id=turn.id
 AND turn.session_id=session.id AND turn.run_id=run.id AND run.session_id=session.id
 AND session.project_id IS NOT DISTINCT FROM run.project_id
 AND node.state='RUNNING' AND run.state IN ('RUNNING','WAITING_HUMAN') AND root.state IN ('RUNNING','WAITING_HUMAN')
 AND (project.lifecycle='ACTIVE' OR (run.project_id IS NULL AND control_plane.owned_organization_assistant_run(lease.organization_id,root.id)))
 AND (@authority_project='' OR project.id=NULLIF(@authority_project,'')::uuid)
 AND invocation.ref=@invocation_ref AND invocation.resource_kind='SSH_TARGET'
 AND connection.definition_key IN('ssh-managed','ssh-execute')
 AND pin.root_actor_id=actor.id AND pin.root_run_id=root.id AND pin.connection_id=invocation.connection_id
 AND EXISTS (SELECT 1 FROM jsonb_array_elements(coalesce(revision.safe_snapshot->'integrationGrants','[]'::jsonb)) binding
  WHERE binding->>'ref'=grant_record.ref AND binding->>'grantVersion'=invocation.grant_version::text
   AND binding->>'capabilityKey'=invocation.operation)
 AND NOT EXISTS (SELECT 1 FROM control_plane.runtime_leases newer WHERE newer.organization_id=lease.organization_id
  AND newer.node_id=node.id AND newer.generation>lease.generation)
FOR SHARE OF lease,actor,agent;
