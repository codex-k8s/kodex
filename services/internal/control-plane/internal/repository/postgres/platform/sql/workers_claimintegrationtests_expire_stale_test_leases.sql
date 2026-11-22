-- name: workers_claimintegrationtests_expire_stale_test_leases :exec
WITH expired AS (
 UPDATE control_plane.integration_connection_tests t
 SET state=CASE WHEN credential_kind='BROKER_IDENTITY' THEN 'CANCELLED' ELSE 'DUE' END,
 lease_ref=NULL,fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL,
 attempt=CASE WHEN credential_kind='BROKER_IDENTITY' THEN attempt ELSE attempt+1 END,
 version=version+1,updated_at=clock_timestamp()
 WHERE organization_id=$1::uuid AND claimed_workload=$2 AND purpose='OWNER_TEST' AND state='CLAIMED' AND lease_expires_at<=clock_timestamp()
 RETURNING t.organization_id,t.connection_id,t.credential_kind,t.broker_identity_revision_id,t.input_snapshot
)
UPDATE control_plane.integration_connections c
SET state=CASE WHEN enabled THEN 'NOT_CONNECTED' ELSE 'DISABLED' END,last_test_summary='',last_tested_at=NULL,version=version+1,updated_at=clock_timestamp()
FROM expired WHERE expired.credential_kind='BROKER_IDENTITY' AND c.organization_id=expired.organization_id AND c.id=expired.connection_id
 AND c.broker_identity_revision_id=expired.broker_identity_revision_id
 AND c.version=(expired.input_snapshot->>'connectionVersion')::bigint AND c.state='TESTING';
