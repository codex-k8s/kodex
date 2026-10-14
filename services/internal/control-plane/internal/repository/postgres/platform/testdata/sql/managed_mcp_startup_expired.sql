INSERT INTO control_plane.integration_connection_tests
  (ref,organization_id,connection_id,state,created_by,purpose,created_at,attempt)
SELECT @test_ref,organization_id,id,'DUE',created_by,'MANAGED_MCP_REFRESH',
  clock_timestamp()-INTERVAL '31 seconds',1
FROM control_plane.integration_connections WHERE ref=@connection_ref;
