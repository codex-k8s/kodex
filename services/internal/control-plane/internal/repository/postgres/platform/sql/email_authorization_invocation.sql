SELECT i.id::text,c.ref,actor.ref,a.ref,p.ref,p.id::text,g.ref,i.operation,i.effect_key,
       i.bounded_input,i.resource_scope,c.public_configuration,i.definition_version,i.definition_digest,
       i.risk,EXISTS(SELECT 1 FROM control_plane.owner_gates gate
                     WHERE gate.integration_invocation_id=i.id AND gate.state='APPROVED')
FROM control_plane.integration_invocations i
JOIN control_plane.integration_connections c ON c.id=i.connection_id AND c.organization_id=i.organization_id
JOIN control_plane.integration_definitions d ON d.stable_key=c.definition_key
JOIN control_plane.integration_grants g ON g.id=i.grant_id AND g.organization_id=i.organization_id
JOIN control_plane.run_nodes n ON n.id=i.node_id AND n.run_id=i.run_id AND n.state='RUNNING'
JOIN control_plane.runs r ON r.id=i.run_id AND r.organization_id=i.organization_id
JOIN control_plane.runs root ON root.id=r.root_run_id AND root.organization_id=i.organization_id
JOIN control_plane.subjects actor ON actor.id=root.initiated_by AND actor.organization_id=i.organization_id
JOIN control_plane.agents a ON a.id=n.agent_id AND a.organization_id=i.organization_id
JOIN control_plane.projects p ON p.id=r.project_id AND p.organization_id=i.organization_id
WHERE i.organization_id=$1::uuid AND i.ref=$2 AND i.state='RUNNING'
  AND i.claimed_workload='integration-gateway' AND i.claimed_lease_ref=$3 AND i.claimed_fence_digest=$4
  AND i.lease_ref=$3 AND i.effect_fence_digest=$4 AND i.generation=$5
  AND i.lease_expires_at=$6 AND i.lease_expires_at>clock_timestamp()
  AND c.enabled AND c.state='CONNECTED' AND c.definition_key='email'
	AND a.enabled AND a.state IN ('READY','RUNNING')
  AND d.enabled AND d.adapter_owner='integration-gateway' AND d.execution_route='MANAGED_MCP' AND d.adapter_readiness='READY'
  AND c.definition_version=i.definition_version AND c.definition_digest=i.definition_digest
  AND g.enabled AND g.target_kind='AGENT' AND g.target_ref=a.ref AND g.capability_key=i.capability_key
  AND g.definition_version=i.definition_version AND g.definition_digest=i.definition_digest
  AND g.resource_scope_digest=i.resource_scope_digest AND g.resource_scope=i.resource_scope
  AND EXISTS(SELECT 1 FROM control_plane.runtime_revisions revision,
      jsonb_array_elements(COALESCE(revision.safe_snapshot->'integrationGrants','[]'::jsonb)) binding
      WHERE revision.node_id=n.id AND revision.organization_id=i.organization_id
        AND revision.generation=(SELECT max(latest.generation) FROM control_plane.runtime_revisions latest WHERE latest.node_id=n.id)
        AND binding->>'ref'=g.ref AND binding->>'capabilityKey'=g.capability_key
		AND binding->>'grantVersion'=g.version::text
        AND binding->>'connectionRef'=c.ref AND binding->>'definitionDigest'=c.definition_digest)
FOR SHARE OF i,c,d,g,n,r,root,actor,a,p;
