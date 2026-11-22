-- name: broker_health__shared_reset :one
UPDATE control_plane.integration_connections c SET state=CASE WHEN enabled THEN 'NOT_CONNECTED' ELSE 'DISABLED' END,version=c.version+1,updated_at=clock_timestamp()
FROM control_plane.integration_connection_tests t
WHERE t.id=@id::uuid AND t.organization_id=@organization_id::uuid AND c.organization_id=t.organization_id AND c.id=t.connection_id
 AND c.state='TESTING' AND c.version=(t.input_snapshot->>'connectionVersion')::bigint
 AND c.broker_identity_revision_id=t.broker_identity_revision_id RETURNING c.ref;
