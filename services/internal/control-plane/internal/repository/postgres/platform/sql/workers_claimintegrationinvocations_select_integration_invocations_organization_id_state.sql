-- name: workers_claimintegrationinvocations_select_integration_invocations_organization_id_state :many
SELECT i.id::text,i.ref,i.generation,c.ref,c.definition_key,c.public_configuration,i.capability_key,i.bounded_input,
	i.definition_version,i.definition_digest,i.operation,i.risk,i.approval_policy,i.resource_kind,i.resource_scope,
	i.resource_scope_digest,i.effect_key,i.input_digest,
	COALESCE(cr.ref,''),COALESCE(cr.revision,0),COALESCE(cr.secret_ref,''),COALESCE(cr.secret_uid::text,''),
	COALESCE(cr.secret_resource_version,''),COALESCE(cr.content_sha256,''),cr.created_at,initiator.ref
FROM control_plane.integration_invocations i
JOIN control_plane.integration_connections c ON c.id=i.connection_id
JOIN control_plane.integration_definitions d ON d.stable_key=c.definition_key
JOIN control_plane.integration_grants g ON g.id=i.grant_id AND g.enabled
JOIN control_plane.run_nodes n ON n.id=i.node_id AND n.state='RUNNING'
JOIN control_plane.runs r ON r.id=i.run_id
JOIN control_plane.runs root ON root.id=r.root_run_id
JOIN control_plane.subjects initiator ON initiator.id=root.initiated_by
LEFT JOIN control_plane.integration_credential_revisions cr ON cr.id=c.credential_revision_id
WHERE i.organization_id=$1::uuid
  AND i.state='READY'
  AND i.operation NOT IN ('mattermost.inbound','mattermost.gate_decisions')
  AND c.enabled AND c.state='CONNECTED'
  AND d.enabled AND d.adapter_owner=$3 AND d.execution_route=$4 AND d.adapter_readiness='READY'
  AND c.definition_version=i.definition_version AND c.definition_digest=i.definition_digest
  AND g.definition_version=i.definition_version AND g.definition_digest=i.definition_digest
  AND g.resource_scope_digest=i.resource_scope_digest AND g.capability_key=i.capability_key
  AND EXISTS (
    SELECT 1 FROM control_plane.runtime_revisions revision,
      jsonb_array_elements(COALESCE(revision.safe_snapshot->'integrationGrants','[]'::jsonb)) binding
    WHERE revision.node_id=n.id AND revision.organization_id=i.organization_id
      AND revision.generation=(SELECT max(latest.generation) FROM control_plane.runtime_revisions latest WHERE latest.node_id=n.id)
      AND binding->>'ref'=g.ref AND binding->>'capabilityKey'=g.capability_key
	  AND binding->>'grantVersion'=g.version::text
  )
  AND (((i.risk='READ' OR (c.definition_key='email' AND i.approval_policy='NONE'))
        AND NOT i.mailbox_gate_required) OR EXISTS(
      SELECT 1 FROM control_plane.owner_gates gate
      WHERE gate.integration_invocation_id=i.id AND gate.state='APPROVED'
  ))
  AND i.effect_receipt_id IS NULL
ORDER BY i.created_at
FOR UPDATE OF i SKIP LOCKED
LIMIT $2
