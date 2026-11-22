-- name: ssh_setup__replace_pending :exec
WITH closed AS (
 UPDATE control_plane.ssh_identity_setups setup
 SET state='CANCELLED',reason_code='OWNER_REPLACED',version=setup.version+1,closed_at=clock_timestamp()
 FROM control_plane.integration_broker_identity_revisions identity
 WHERE identity.id=setup.identity_revision_id AND identity.organization_id=setup.organization_id
  AND identity.connection_id=setup.connection_id AND identity.purpose='SSH_TARGET'
  AND identity.subject_id=@subject_id::uuid AND identity.credential_policy=@policy
  AND setup.organization_id=@organization_id::uuid AND setup.connection_id=@connection_id::uuid
  AND setup.state IN('PREPARED','ATTEMPTING','UNKNOWN')
 RETURNING setup.id,setup.organization_id
)
UPDATE control_plane.ssh_identity_upload_attempts attempt
SET state='CLOSED',closed_at=clock_timestamp(),close_reason='SETUP_REPLACED'
FROM closed WHERE attempt.setup_id=closed.id AND attempt.organization_id=closed.organization_id AND attempt.state='ACTIVE';
