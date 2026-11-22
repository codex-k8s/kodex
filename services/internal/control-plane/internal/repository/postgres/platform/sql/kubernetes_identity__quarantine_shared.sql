-- name: kubernetes_identity__quarantine_shared :exec
UPDATE control_plane.integration_connections
SET state=CASE WHEN enabled THEN 'NOT_CONNECTED' ELSE 'DISABLED' END,
 last_tested_at=NULL,last_test_summary='',version=version+1,updated_at=clock_timestamp()
WHERE organization_id=@organization_id::uuid AND id=@connection_id::uuid
 AND broker_identity_revision_id=@identity_id::uuid AND credential_policy='PROJECT_SHARED';
