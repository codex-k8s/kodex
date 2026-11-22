-- name: email_credential_owner__set :one
UPDATE control_plane.integration_connections connection
SET mailbox_owner_subject_id=subject.id,
    mailbox_owner_configured_by=@operator_id::uuid,
    mailbox_owner_generation=connection.mailbox_owner_generation+1,
    mailbox_owner_state=@state,
    credential_policy=@policy,
    version=connection.version+1,
    masked_credentials_state='NOT_CONFIGURED',last_tested_at=NULL,last_test_summary='',
    state=CASE WHEN @state='REVOKED' AND connection.enabled THEN 'NOT_CONNECTED' ELSE connection.state END,
    updated_at=clock_timestamp()
FROM control_plane.subjects subject
WHERE connection.id=@connection_id::uuid AND connection.organization_id=@organization_id::uuid
  AND connection.definition_key='email' AND connection.lifecycle_state='ACTIVE'
  AND connection.version=@expected_version
  AND subject.organization_id=connection.organization_id AND subject.id=@subject_id::uuid
  AND subject.kind=@subject_kind
  AND (@state='REVOKED' OR subject.active)
RETURNING connection.version,connection.mailbox_owner_generation;
