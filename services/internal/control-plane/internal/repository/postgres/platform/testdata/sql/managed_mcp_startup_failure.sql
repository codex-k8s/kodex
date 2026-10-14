INSERT INTO control_plane.integration_connection_tests
  (ref,organization_id,connection_id,state,generation,claimed_workload,completed_at,created_by)
SELECT @test_ref,organization_id,id,'FAILED',1,'integration-gateway',clock_timestamp(),created_by
FROM control_plane.integration_connections WHERE ref=@connection_ref;
