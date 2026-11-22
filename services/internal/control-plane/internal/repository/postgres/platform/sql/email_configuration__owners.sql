-- name: email_configuration__owners :many
SELECT organization.ref,connection.ref
FROM jsonb_to_recordset(@mailboxes::jsonb) AS mailbox(
  tenant_id text,connection_id text,id text,enabled boolean,
  credential_owner_generation bigint,credential_generation bigint)
JOIN control_plane.organizations organization ON organization.ref=mailbox.tenant_id
JOIN control_plane.integration_connections connection ON connection.organization_id=organization.id
  AND connection.ref=mailbox.connection_id AND connection.definition_key='email'
JOIN control_plane.email_mailbox_configuration_sets assignment ON assignment.connection_id=connection.id
  AND assignment.organization_id=connection.organization_id AND assignment.mailbox_ref=mailbox.id
JOIN control_plane.subjects subject ON subject.id=connection.mailbox_owner_subject_id
  AND subject.organization_id=connection.organization_id AND subject.active
JOIN control_plane.subjects configurer ON configurer.id=connection.mailbox_owner_configured_by
  AND configurer.organization_id=connection.organization_id AND configurer.kind='USER' AND configurer.active
WHERE mailbox.enabled AND connection.enabled AND connection.lifecycle_state='ACTIVE'
  AND connection.mailbox_owner_state='ACTIVE'
  AND mailbox.credential_owner_generation=connection.mailbox_owner_generation
  AND mailbox.credential_generation>=connection.mailbox_credential_version
  AND ((connection.credential_policy='INITIATOR_REQUIRED' AND subject.kind='USER' AND subject.id=configurer.id)
    OR (connection.credential_policy='PROJECT_SHARED' AND subject.kind='SERVICE'))
ORDER BY organization.ref,connection.ref
FOR SHARE OF connection,subject,configurer;
