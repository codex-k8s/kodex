-- name: email_authorization_fixture_state :one
SELECT encode(sha256(convert_to(jsonb_build_object(
  'test',jsonb_build_array(test.ref,test.state,test.generation,test.lease_ref,test.fence_digest,
    test.workload_instance,test.lease_expires_at,test.input_snapshot),
  'connection',jsonb_build_array(connection.ref,connection.version,connection.state,connection.mailbox_owner_generation),
  'authorizations',(SELECT coalesce(jsonb_agg(jsonb_build_array(auth.ref,auth.query_projection,
    auth.decision_projection,auth.expires_at) ORDER BY auth.ref),'[]'::jsonb)
    FROM control_plane.email_authorizations auth WHERE auth.organization_id=test.organization_id AND auth.connection_test_id=test.id)
)::text,'UTF8')),'hex')
FROM control_plane.integration_connection_tests test
JOIN control_plane.integration_connections connection ON connection.id=test.connection_id AND connection.organization_id=test.organization_id
WHERE test.organization_id=@organization_id::uuid AND test.ref=@test_ref;
