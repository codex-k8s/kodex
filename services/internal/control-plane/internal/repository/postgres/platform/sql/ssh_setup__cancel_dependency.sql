-- name: ssh_setup__cancel_dependency :exec
WITH closed AS (
 UPDATE control_plane.ssh_identity_setups setup SET state='CANCELLED',reason_code='OWNER_REPLACED',version=setup.version+1,closed_at=clock_timestamp()
 FROM control_plane.integration_connections connection,control_plane.integration_broker_identity_revisions identity
 WHERE setup.organization_id=@organization_id::uuid AND connection.organization_id=setup.organization_id AND connection.id=setup.connection_id
  AND connection.ref=@connection_ref AND identity.id=setup.identity_revision_id AND identity.organization_id=setup.organization_id
  AND identity.connection_id=connection.id AND identity.purpose='SSH_TARGET'
  AND (@binding_id::text='' OR identity.binding_id=nullif(@binding_id,'')::uuid)
  AND setup.state IN('PREPARED','ATTEMPTING','UNKNOWN') RETURNING setup.id,setup.organization_id
)
UPDATE control_plane.ssh_identity_upload_attempts attempt
SET state='CLOSED',closed_at=clock_timestamp(),close_reason='OWNER_REPLACED'
FROM closed WHERE attempt.setup_id=closed.id AND attempt.organization_id=closed.organization_id AND attempt.state='ACTIVE';
