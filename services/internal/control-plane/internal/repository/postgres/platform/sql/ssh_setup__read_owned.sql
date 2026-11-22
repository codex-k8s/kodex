-- name: ssh_setup__read_owned :one
WITH observed AS MATERIALIZED (SELECT clock_timestamp() AS now)
SELECT setup.ref,connection.ref,identity.ref,identity.target_ref,
 CASE WHEN setup.state IN('PREPARED','ATTEMPTING','UNKNOWN') AND observed.now>=setup.expires_at
  THEN CASE WHEN EXISTS(SELECT 1 FROM control_plane.ssh_identity_upload_attempts attempt
    WHERE attempt.setup_id=setup.id AND attempt.organization_id=setup.organization_id AND attempt.mode='WRITE')
   THEN 'SETUP_EXPIRED_WITH_MATERIAL_UNRESOLVED' ELSE 'SETUP_EXPIRED' END
  ELSE setup.reason_code END,
 setup.version,setup.expected_connection_version,setup.expected_binding_version,
 setup.expected_binding_generation,identity.revision,identity.credential_policy,
 CASE WHEN setup.state IN('PREPARED','ATTEMPTING','UNKNOWN') AND observed.now>=setup.expires_at
  THEN 'EXPIRED' ELSE setup.state END,setup.expires_at
FROM control_plane.ssh_identity_setups setup
JOIN control_plane.subjects actor ON actor.id=setup.actor_id AND actor.organization_id=setup.organization_id
 AND actor.kind='USER' AND actor.active
JOIN control_plane.integration_connections connection ON connection.id=setup.connection_id
 AND connection.organization_id=setup.organization_id AND connection.lifecycle_state='ACTIVE'
JOIN control_plane.integration_broker_identity_revisions identity ON identity.id=setup.identity_revision_id
 AND identity.organization_id=setup.organization_id AND identity.connection_id=setup.connection_id AND identity.purpose='SSH_TARGET'
CROSS JOIN observed
WHERE setup.organization_id=@organization_id::uuid AND setup.actor_id=@actor_id::uuid
 AND connection.ref=@connection_ref
 AND ((@setup_ref::text<>'' AND setup.ref=@setup_ref) OR (@begin_key::text<>'' AND setup.begin_idempotency_key=@begin_key));
