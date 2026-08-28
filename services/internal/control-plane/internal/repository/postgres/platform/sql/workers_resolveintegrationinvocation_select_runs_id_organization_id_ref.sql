-- name: workers_resolveintegrationinvocation_select_runs_id_organization_id_ref :one
SELECT r.id::text,n.id::text,c.id::text,g.id::text,g.ref,r.project_id::text,r.root_run_id::text,
	c.definition_key,c.definition_version,c.definition_digest,
	g.risk,g.approval_policy,g.resource_kind,g.resource_scope,g.resource_scope_digest
FROM control_plane.runs r
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
WHERE r.organization_id=$1::uuid AND r.ref=$2 AND n.ref=$3 AND n.state='RUNNING'
FOR UPDATE OF n,c,g
