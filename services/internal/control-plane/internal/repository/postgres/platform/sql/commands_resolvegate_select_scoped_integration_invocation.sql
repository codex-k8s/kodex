-- name: commands_resolvegate_select_scoped_integration_invocation :one
SELECT i.approval_policy,i.bounded_input,i.input_digest,i.capability_key,
       i.definition_version,i.definition_digest,c.ref,c.definition_key,
       i.organization_id::text,COALESCE(r.project_id::text,''),r.root_run_id::text,
       COALESCE(n.agent_id::text,''),i.connection_id::text,i.grant_id::text,
       i.grant_version,i.approval_scope_paths
FROM control_plane.integration_invocations i
JOIN control_plane.integration_connections c ON c.id=i.connection_id
JOIN control_plane.integration_grants g ON g.id=i.grant_id
JOIN control_plane.runs r ON r.id=i.run_id
JOIN control_plane.runs root ON root.id=r.root_run_id
JOIN control_plane.run_nodes n ON n.id=i.node_id
JOIN control_plane.owner_gates gate ON gate.id=$5::uuid AND gate.integration_invocation_id=i.id
JOIN control_plane.integration_definitions d ON d.stable_key=c.definition_key
WHERE i.id=$1::uuid AND i.organization_id=$2::uuid AND i.state='WAITING_APPROVAL'
  AND r.root_run_id=$3::uuid AND r.project_id IS NOT DISTINCT FROM NULLIF($4,'')::uuid
  AND gate.organization_id=i.organization_id AND gate.project_id IS NOT DISTINCT FROM r.project_id
  AND (gate.scope_kind='PROJECT' OR (gate.scope_kind='ORGANIZATION'
       AND control_plane.owned_organization_assistant_run(gate.organization_id,r.root_run_id)))
  AND gate.root_run_id=root.id AND gate.state='OPEN'
  AND root.state='WAITING_HUMAN' AND n.state IN ('RUNNING','SUCCEEDED')
  AND c.enabled AND c.state='CONNECTED' AND d.enabled AND d.adapter_readiness='READY'
  AND g.enabled AND g.approval_policy=i.approval_policy
  AND g.capability_key=i.capability_key
  AND g.version=i.grant_version AND g.approval_scope_paths=i.approval_scope_paths
  AND g.definition_version=i.definition_version AND g.definition_digest=i.definition_digest
  AND c.definition_version=i.definition_version AND c.definition_digest=i.definition_digest
FOR UPDATE OF i,g,c
