-- name: email_pending__close_tests :many
WITH candidates AS MATERIALIZED (
 SELECT test.id FROM control_plane.integration_connection_tests test
 JOIN control_plane.integration_connections connection ON connection.id=test.connection_id
   AND connection.organization_id=test.organization_id AND connection.definition_key='email'
 WHERE test.organization_id=@organization_id::uuid AND test.state IN ('DUE','CLAIMED')
   AND (@connection_ref='' OR connection.ref=@connection_ref)
   AND NOT control_plane.email_connection_test_mailbox_current(test.id)
 ORDER BY test.created_at,test.id FOR UPDATE OF test SKIP LOCKED LIMIT 100
), closed AS (
 UPDATE control_plane.integration_connection_tests test
 SET state='CANCELLED',lease_ref=NULL,fence_digest=NULL,workload_instance=NULL,
   lease_expires_at=NULL,completed_at=clock_timestamp(),version=test.version+1,updated_at=clock_timestamp()
 FROM candidates WHERE test.id=candidates.id RETURNING test.connection_id
)
UPDATE control_plane.integration_connections connection
SET state=CASE WHEN enabled THEN 'NOT_CONNECTED' ELSE 'DISABLED' END,last_test_summary='',
  version=connection.version+1,updated_at=clock_timestamp()
WHERE connection.organization_id=@organization_id::uuid AND connection.definition_key='email'
  AND connection.id IN (SELECT connection_id FROM closed) AND connection.state='TESTING'
RETURNING connection.ref,connection.version,connection.state;
