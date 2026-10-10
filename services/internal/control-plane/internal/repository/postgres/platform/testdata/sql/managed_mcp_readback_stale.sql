-- Новый synthetic receipt с заданным возрастом; immutable history не меняется.
-- Input snapshot назначает существующий owner trigger.
INSERT INTO control_plane.integration_connection_tests
  (ref,organization_id,connection_id,state,generation,claimed_workload,completed_at,created_by)
SELECT 'tst_readback_stale',organization_id,id,'SUCCEEDED',1,'integration-gateway',
  clock_timestamp()-INTERVAL '6 minutes',created_by
FROM control_plane.integration_connections WHERE ref=@connection_ref;
