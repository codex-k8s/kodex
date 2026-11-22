-- name: kubernetes_setup__cancel_dependency :exec
WITH selected AS MATERIALIZED (
 SELECT setup.id FROM control_plane.kubernetes_identity_setups setup
 JOIN control_plane.integration_broker_identity_revisions identity ON identity.id=setup.identity_revision_id
  AND identity.organization_id=setup.organization_id
 JOIN control_plane.integration_connections connection ON connection.id=setup.connection_id
  AND connection.organization_id=setup.organization_id
 WHERE setup.organization_id=@organization_id::uuid AND connection.ref=@connection_ref
  AND (@binding_id='' OR identity.binding_id=NULLIF(@binding_id,'')::uuid)
  AND setup.state IN('PREPARED','ATTEMPTING','UNKNOWN')
 ORDER BY setup.id FOR UPDATE OF setup
), closed_attempts AS (
 UPDATE control_plane.kubernetes_identity_upload_attempts attempt
 SET state='CANCELLED',closed_at=clock_timestamp()
 FROM selected WHERE attempt.setup_id=selected.id AND attempt.organization_id=@organization_id::uuid
  AND attempt.state='ACTIVE' RETURNING attempt.id
)
UPDATE control_plane.kubernetes_identity_setups setup SET state='CANCELLED',version=version+1
FROM selected WHERE setup.id=selected.id AND setup.organization_id=@organization_id::uuid;
