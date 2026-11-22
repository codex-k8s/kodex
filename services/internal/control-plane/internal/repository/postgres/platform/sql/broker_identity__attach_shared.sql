-- name: broker_identity__attach_shared :exec
UPDATE control_plane.integration_connections connection
SET broker_identity_revision_id=@identity_id::uuid,credential_revision_id=NULL,credential_materialization_ref=NULL,
 masked_credentials_state='CONFIGURED',state=CASE WHEN enabled THEN 'NOT_CONNECTED' ELSE 'DISABLED' END,
 last_test_summary='',last_tested_at=NULL,version=version+1,updated_at=clock_timestamp()
WHERE connection.organization_id=@organization_id::uuid AND connection.id=@connection_id::uuid
 AND connection.version=@version AND connection.credential_policy='PROJECT_SHARED' AND connection.lifecycle_state='ACTIVE'
 AND clock_timestamp()<@authorized_until::timestamptz
 AND (connection.broker_identity_revision_id IS NULL OR EXISTS(SELECT 1 FROM control_plane.integration_broker_identity_revisions previous
  WHERE previous.id=connection.broker_identity_revision_id AND previous.organization_id=connection.organization_id
  AND previous.connection_id=connection.id AND previous.purpose=@purpose
  AND (previous.subject_id<>@subject_id::uuid OR previous.revision<@revision)));
