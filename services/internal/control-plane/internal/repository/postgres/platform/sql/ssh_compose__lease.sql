-- name: ssh_compose__lease :one
SELECT i.id::text,i.node_id::text,r.project_id::text,i.connection_id::text,
       i.operation,i.bounded_input,i.ssh_compose_snapshot,i.ssh_compose_deadline,
       i.effect_key,i.input_digest,initiator.ref,i.lease_expires_at,i.state,
       i.ref,i.lease_ref,i.generation,c.ref,r.id::text,i.ssh_compose_recovery
FROM control_plane.integration_invocations i
JOIN control_plane.integration_connections c ON c.id=i.connection_id
JOIN control_plane.integration_definitions d ON d.stable_key=c.definition_key
JOIN control_plane.integration_grants g ON g.id=i.grant_id AND g.enabled
JOIN control_plane.run_nodes n ON n.id=i.node_id
JOIN control_plane.runs r ON r.id=i.run_id
JOIN control_plane.runs root ON root.id=r.root_run_id
JOIN control_plane.projects project ON project.id=r.project_id AND project.lifecycle='ACTIVE'
JOIN control_plane.subjects initiator ON initiator.id=root.initiated_by
WHERE i.organization_id=@organization_id::uuid AND i.ref=@invocation_ref
  AND i.lease_ref=@lease_ref AND i.generation=@generation
  AND i.effect_fence_digest=@fence_digest AND i.workload_instance=@workload_instance
  AND i.state='RUNNING' AND i.claimed_workload='integration-gateway'
  AND i.lease_expires_at>clock_timestamp()
  AND (i.ssh_compose_recovery OR i.ssh_compose_deadline IS NULL OR i.ssh_compose_deadline>clock_timestamp())
  AND control_plane.runtime_execution_before_deadline(i.organization_id,i.run_id)
  AND root.state IN ('RUNNING','WAITING_HUMAN')
  AND n.state IN ('RUNNING','SUCCEEDED')
  AND c.definition_key='ssh-compose' AND c.enabled AND c.state='CONNECTED'
  AND d.enabled AND d.adapter_readiness='READY' AND d.adapter_owner='integration-gateway'
  AND c.definition_version=i.definition_version AND c.definition_digest=i.definition_digest
  AND g.version=i.grant_version AND g.definition_version=i.definition_version AND g.definition_digest=i.definition_digest
  AND g.resource_scope_digest=i.resource_scope_digest AND g.approval_policy=i.approval_policy
  AND g.target_kind='AGENT' AND g.target_ref=(SELECT a.ref FROM control_plane.agents a WHERE a.id=n.agent_id)
  AND ((i.risk='READ' AND i.approval_policy='NONE') OR
    (i.operation='ssh.compose.deploy' AND i.risk='WRITE' AND i.approval_policy='HUMAN_EACH_EFFECT' AND EXISTS (
      SELECT 1 FROM control_plane.owner_gates gate WHERE gate.integration_invocation_id=i.id
        AND gate.organization_id=i.organization_id AND gate.root_run_id=r.root_run_id
        AND gate.project_id=r.project_id AND gate.state='APPROVED'
    )))
  AND EXISTS (
    SELECT 1 FROM control_plane.runtime_revisions revision,
      jsonb_array_elements(COALESCE(revision.safe_snapshot->'integrationGrants','[]'::jsonb)) binding
    WHERE revision.organization_id=i.organization_id AND revision.node_id=n.id
      AND revision.id::text=i.ssh_compose_snapshot->>'originRuntimeRevisionId'
      AND revision.generation=(i.ssh_compose_snapshot->>'originGeneration')::bigint
      AND revision.run_id=n.run_id AND revision.root_run_id=n.root_run_id
      AND revision.agent_id=n.agent_id AND revision.turn_id=n.turn_id AND revision.attempt=n.attempt
      AND binding->>'ref'=g.ref AND binding->>'grantVersion'=g.version::text
      AND binding->>'capabilityKey'=g.capability_key AND binding->>'approvalPolicy'=g.approval_policy
  )
FOR UPDATE OF i NOWAIT FOR SHARE OF c,g NOWAIT;
