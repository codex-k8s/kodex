-- name: ssh_compose__invalidate_health :many
WITH closed AS (
  UPDATE control_plane.integration_connection_tests test
  SET state='CANCELLED',lease_ref=NULL,fence_digest=NULL,workload_instance=NULL,
      lease_expires_at=NULL,completed_at=clock_timestamp(),version=test.version+1,updated_at=clock_timestamp()
  FROM control_plane.integration_connections connection
  WHERE test.connection_id=connection.id AND test.organization_id=@organization_id::uuid
    AND connection.definition_key='ssh-compose' AND test.state IN ('DUE','CLAIMED')
    AND ((@connection_ref<>'' AND connection.ref=@connection_ref)
      OR (@secret_ref<>'' AND connection.public_configuration->>'ssh_secret_ref'=@secret_ref))
  RETURNING test.connection_id
), reset AS (
  UPDATE control_plane.integration_connections connection
  SET state=CASE WHEN enabled THEN 'NOT_CONNECTED' ELSE 'DISABLED' END,last_test_summary='',
      version=connection.version+1,updated_at=clock_timestamp()
  WHERE connection.organization_id=@organization_id::uuid AND connection.definition_key='ssh-compose'
    AND ((connection.id IN (SELECT connection_id FROM closed) AND connection.state='TESTING')
      OR (@secret_ref<>'' AND connection.public_configuration->>'ssh_secret_ref'=@secret_ref
        AND connection.lifecycle_state='ACTIVE' AND connection.enabled))
  RETURNING connection.ref,connection.organization_id,connection.public_configuration,connection.version,connection.state
)
SELECT reset.ref,reset.version,reset.state,
       organization.ref,actor.ref,actor.display_name
FROM reset
JOIN control_plane.organizations organization ON organization.id=reset.organization_id
JOIN control_plane.subjects actor ON actor.id=@actor_id::uuid AND actor.organization_id=organization.id
ORDER BY reset.ref;
