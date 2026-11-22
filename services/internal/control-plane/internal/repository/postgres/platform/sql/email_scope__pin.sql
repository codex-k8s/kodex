-- name: email_scope__pin :exec
INSERT INTO control_plane.email_approval_scope_bindings(approval_scope_id,organization_id,connection_id,
  owner_subject_id,owner_generation,mailbox_ref,mailbox_revision,mailbox_source_digest)
SELECT approval.id,pin.organization_id,pin.connection_id,pin.owner_subject_id,pin.owner_generation,
       pin.mailbox_ref,pin.mailbox_revision,pin.mailbox_source_digest
FROM control_plane.email_invocation_mailbox_bindings pin
JOIN control_plane.integration_invocations invocation ON invocation.id=pin.invocation_id
  AND invocation.organization_id=pin.organization_id AND invocation.approval_policy='HUMAN_SCOPED'
JOIN control_plane.integration_approval_scopes approval ON approval.id=@approval_scope_id::uuid
  AND approval.organization_id=pin.organization_id AND approval.connection_id=pin.connection_id
  AND approval.grant_id=invocation.grant_id AND approval.agent_id=(
    SELECT node.agent_id FROM control_plane.run_nodes node WHERE node.id=invocation.node_id)
JOIN control_plane.integration_connections connection ON connection.id=pin.connection_id
  AND connection.organization_id=pin.organization_id AND connection.mailbox_owner_state='ACTIVE'
  AND connection.mailbox_owner_subject_id=pin.owner_subject_id AND connection.mailbox_owner_generation=pin.owner_generation
JOIN control_plane.email_mailbox_projections mailbox ON mailbox.organization_id=pin.organization_id
  AND mailbox.connection_id=pin.connection_id AND mailbox.ref=pin.mailbox_ref AND mailbox.enabled AND NOT mailbox.removed
  AND mailbox.revision=pin.mailbox_revision AND mailbox.safe_projection->>'source_digest'=pin.mailbox_source_digest
  AND mailbox.safe_projection->>'credential_owner_generation'=pin.owner_generation::text
  AND (mailbox.safe_projection->>'credential_generation')::bigint>=connection.mailbox_credential_version
JOIN control_plane.email_configuration_watermark watermark ON watermark.singleton AND watermark.revision=mailbox.document_revision
JOIN control_plane.runs root ON root.id=pin.root_run_id AND root.organization_id=pin.organization_id
  AND root.root_run_id=root.id AND root.initiated_by=pin.initiator_id AND root.state IN ('RUNNING','WAITING_HUMAN')
JOIN control_plane.subjects actor ON actor.id=pin.initiator_id AND actor.organization_id=pin.organization_id AND actor.active
JOIN control_plane.subjects owner ON owner.id=pin.owner_subject_id AND owner.organization_id=pin.organization_id AND owner.active
JOIN control_plane.subjects configurer ON configurer.id=connection.mailbox_owner_configured_by
  AND configurer.organization_id=pin.organization_id AND configurer.kind='USER' AND configurer.active
WHERE pin.invocation_id=@invocation_id::uuid AND pin.organization_id=@organization_id::uuid
  AND ((connection.credential_policy='INITIATOR_REQUIRED' AND actor.kind='USER' AND actor.id=owner.id
    AND owner.kind='USER' AND owner.id=configurer.id AND root.source NOT IN ('SCHEDULE','INTEGRATION'))
    OR (connection.credential_policy='PROJECT_SHARED' AND owner.kind='SERVICE'
      AND (actor.kind='USER' OR actor.kind='SERVICE' AND actor.id=owner.id)));
