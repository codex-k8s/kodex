-- name: workers_claimintegrationinvocations_select_integration_invocations_organization_id_state :many
SELECT i.id::text,i.ref,i.generation,c.ref,c.definition_key,c.public_configuration,i.capability_key,i.bounded_input,
	i.definition_version,i.definition_digest,i.operation,i.risk,i.approval_policy,i.resource_kind,i.resource_scope,
	i.resource_scope_digest,i.effect_key,i.input_digest,
	COALESCE(cr.ref,''),COALESCE(cr.revision,0),COALESCE(cr.secret_ref,''),COALESCE(cr.secret_uid::text,''),
	COALESCE(cr.secret_resource_version,''),COALESCE(cr.content_sha256,''),cr.created_at
FROM control_plane.integration_invocations i
JOIN control_plane.integration_connections c ON c.id=i.connection_id
LEFT JOIN control_plane.integration_credential_revisions cr ON cr.id=c.credential_revision_id
WHERE i.organization_id=$1::uuid
  AND i.state='READY'
  AND c.enabled AND c.state='CONNECTED'
  AND c.definition_version=i.definition_version AND c.definition_digest=i.definition_digest
  AND (i.risk='READ' OR EXISTS(
      SELECT 1 FROM control_plane.owner_gates gate
      WHERE gate.integration_invocation_id=i.id AND gate.state='APPROVED'
  ))
  AND i.effect_receipt_id IS NULL
ORDER BY i.created_at
FOR UPDATE OF i SKIP LOCKED
LIMIT $2
