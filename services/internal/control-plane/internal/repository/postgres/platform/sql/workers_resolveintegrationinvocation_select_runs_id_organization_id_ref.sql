-- name: workers_resolveintegrationinvocation_select_runs_id_organization_id_ref :one
SELECT r.id::text,n.id::text,c.id::text,g.id::text,g.ref,r.project_id::text,r.root_run_id::text,
	c.definition_key,c.definition_version,c.definition_digest,
	g.risk,g.approval_policy,g.resource_kind,g.resource_scope,g.resource_scope_digest,initiator.ref
FROM control_plane.runs r
JOIN control_plane.runs root ON root.id=r.root_run_id
JOIN control_plane.subjects initiator ON initiator.id=root.initiated_by
JOIN control_plane.run_nodes n ON n.run_id=r.id
JOIN control_plane.integration_connections c
  ON c.organization_id=r.organization_id AND c.ref=$4 AND c.enabled AND c.state='CONNECTED'
JOIN control_plane.integration_grants g
  ON g.connection_id=c.id AND g.capability_key=$5
 AND g.target_kind='AGENT'
 AND g.target_ref=(SELECT a.ref FROM control_plane.agents a WHERE a.id=n.agent_id)
 AND g.enabled
 AND g.definition_version=c.definition_version
 AND g.definition_digest=c.definition_digest
JOIN control_plane.integration_definitions d ON d.stable_key=c.definition_key
WHERE r.organization_id=$1::uuid AND r.ref=$2 AND n.ref=$3 AND n.state='RUNNING'
  AND d.enabled AND (d.adapter_owner,d.execution_route) IN
      (('integration-gateway','MANAGED_MCP'),('interaction-gateway','INTERACTION'))
  AND d.adapter_readiness='READY'
  AND EXISTS (
      SELECT 1 FROM jsonb_array_elements(d.capabilities) capability
      WHERE capability->>'key'=g.capability_key
        AND capability->>'operation' NOT IN ('mattermost.inbound','mattermost.gate_decisions')
  )
  AND EXISTS (
    SELECT 1 FROM control_plane.runtime_revisions revision,
      jsonb_array_elements(COALESCE(revision.safe_snapshot->'integrationGrants','[]'::jsonb)) binding
    WHERE revision.node_id=n.id AND revision.organization_id=r.organization_id
      AND revision.generation=(SELECT max(latest.generation) FROM control_plane.runtime_revisions latest WHERE latest.node_id=n.id)
      AND binding->>'ref'=g.ref AND binding->>'capabilityKey'=g.capability_key
	  AND binding->>'grantVersion'=g.version::text
  )
FOR UPDATE OF n,c,g
