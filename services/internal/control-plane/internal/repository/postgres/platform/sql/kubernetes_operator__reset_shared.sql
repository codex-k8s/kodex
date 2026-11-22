-- name: kubernetes_operator__reset_shared :exec
UPDATE control_plane.integration_connections connection
SET state=CASE WHEN connection.enabled THEN 'NOT_CONNECTED' ELSE 'DISABLED' END,
 last_test_summary='',last_tested_at=NULL,version=connection.version+1,updated_at=clock_timestamp()
FROM control_plane.integration_broker_identity_revisions identity
WHERE identity.id=connection.broker_identity_revision_id AND identity.organization_id=connection.organization_id
 AND identity.purpose='KUBERNETES_TARGET' AND connection.lifecycle_state='ACTIVE';
