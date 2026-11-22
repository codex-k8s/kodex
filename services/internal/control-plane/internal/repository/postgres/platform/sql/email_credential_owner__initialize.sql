-- name: email_credential_owner__initialize :exec
UPDATE control_plane.integration_connections connection
SET mailbox_owner_subject_id=subject.id,mailbox_owner_configured_by=subject.id,
    mailbox_owner_generation=1,mailbox_owner_state='ACTIVE'
FROM control_plane.subjects subject
WHERE connection.id=@connection_id::uuid AND connection.organization_id=@organization_id::uuid
  AND connection.definition_key='email' AND connection.credential_policy='INITIATOR_REQUIRED'
  AND connection.mailbox_owner_state='UNCONFIGURED' AND connection.mailbox_owner_generation=0
  AND connection.created_by=subject.id
  AND subject.id=@subject_id::uuid AND subject.organization_id=connection.organization_id
  AND subject.kind='USER' AND subject.active;
