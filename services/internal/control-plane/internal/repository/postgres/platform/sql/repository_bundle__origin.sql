-- name: repository_bundle__origin :one
SELECT run.id::text,run.ref,run.root_run_id::text,run.project_id::text,project.ref,
       initiator.id::text,initiator.ref,node.id::text,node.ref,revision.id::text,revision.ref,
       connection.id::text,connection.ref,grant_binding.id::text,grant_binding.version,
       grant_binding.resource_scope_digest,grant_binding.resource_scope
FROM control_plane.runtime_leases lease
JOIN control_plane.runtime_revisions revision
  ON revision.id=lease.runtime_revision_id AND revision.organization_id=lease.organization_id
 AND revision.run_id=lease.run_id AND revision.node_id=lease.node_id
 AND revision.generation=lease.generation AND revision.input_digest=lease.input_digest
JOIN control_plane.run_nodes node
  ON node.id=lease.node_id AND node.organization_id=lease.organization_id
 AND node.agent_id=revision.agent_id AND node.attempt=revision.attempt
 AND node.turn_id IS NOT DISTINCT FROM revision.turn_id AND node.state='RUNNING'
JOIN control_plane.runs run
  ON run.id=lease.run_id AND run.organization_id=lease.organization_id
 AND run.root_run_id=revision.root_run_id AND run.project_id=revision.project_id
JOIN control_plane.runs root
  ON root.id=run.root_run_id AND root.organization_id=run.organization_id
 AND root.state IN ('RUNNING','WAITING_HUMAN')
JOIN control_plane.projects project ON project.id=run.project_id AND project.organization_id=run.organization_id
JOIN control_plane.subjects initiator ON initiator.id=root.initiated_by AND initiator.organization_id=run.organization_id AND initiator.active
JOIN control_plane.agents agent ON agent.id=node.agent_id AND agent.organization_id=run.organization_id AND agent.project_id=run.project_id
JOIN control_plane.integration_grants grant_binding
  ON grant_binding.organization_id=run.organization_id AND grant_binding.ref=@grant_ref
 AND grant_binding.target_kind='AGENT' AND grant_binding.target_ref=agent.ref
 AND grant_binding.enabled AND grant_binding.capability_key='github.repository.bundle.publish'
 AND grant_binding.risk='WRITE' AND grant_binding.approval_policy='HUMAN_EACH_EFFECT'
JOIN control_plane.integration_connections connection
  ON connection.id=grant_binding.connection_id AND connection.organization_id=grant_binding.organization_id
 AND connection.enabled AND connection.state='CONNECTED' AND connection.definition_key='github'
 AND connection.definition_version=grant_binding.definition_version AND connection.definition_digest=grant_binding.definition_digest
WHERE lease.organization_id=@organization_id::uuid AND lease.ref=@lease_ref
  AND agent.system_key IS NULL
  AND NOT EXISTS (SELECT 1 FROM control_plane.project_assistant_profiles profile
                  WHERE profile.organization_id=agent.organization_id AND profile.agent_id=agent.id)
  AND lease.fence_digest=@fence_digest AND lease.generation=@generation
  AND lease.state='CLAIMED' AND lease.expires_at>clock_timestamp()
  AND NOT EXISTS (SELECT 1 FROM control_plane.callback_receipts delivered
    WHERE delivered.child_run_id=run.id)
  AND control_plane.runtime_execution_before_deadline(lease.organization_id,lease.run_id)
  AND EXISTS (
    SELECT 1 FROM jsonb_array_elements(COALESCE(revision.safe_snapshot->'integrationGrants','[]'::jsonb)) pin
    WHERE pin->>'ref'=grant_binding.ref AND pin->>'grantVersion'=grant_binding.version::text
      AND pin->>'capabilityKey'=grant_binding.capability_key AND pin->>'approvalPolicy'=grant_binding.approval_policy
  )
FOR UPDATE OF node,connection,grant_binding;
