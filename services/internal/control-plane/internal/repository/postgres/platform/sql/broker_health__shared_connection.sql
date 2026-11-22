-- name: broker_health__shared_connection :one
UPDATE control_plane.integration_connections SET state=@state,masked_credentials_state=CASE WHEN @authenticated THEN 'CONFIGURED' ELSE masked_credentials_state END,
last_test_summary=@summary,last_tested_at=clock_timestamp(),version=version+1,updated_at=clock_timestamp()
WHERE organization_id=@organization_id::uuid AND ref=@connection_ref AND state='TESTING' AND enabled
 AND broker_identity_revision_id=@identity_id::uuid AND clock_timestamp()<@deadline RETURNING ref;
