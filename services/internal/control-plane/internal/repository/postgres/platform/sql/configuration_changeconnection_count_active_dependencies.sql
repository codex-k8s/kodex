-- name: configuration_changeconnection_count_active_dependencies :one
SELECT
    (SELECT count(*)
     FROM control_plane.integration_connection_tests test
     WHERE test.connection_id = @connection_id::uuid
       AND test.state IN ('DUE', 'CLAIMED'))
  + (SELECT count(*)
     FROM control_plane.integration_invocations invocation
     WHERE invocation.connection_id = @connection_id::uuid
       AND invocation.state IN ('WAITING_APPROVAL', 'READY', 'RUNNING', 'UNKNOWN_OUTCOME'))
  + (SELECT count(*)
     FROM control_plane.interaction_deliveries delivery
     WHERE delivery.connection_id = @connection_id::uuid
       AND delivery.state IN ('WAITING_APPROVAL', 'DUE', 'CLAIMED', 'FAILED', 'UNKNOWN_OUTCOME')) AS active_effects,
    (SELECT count(*)
     FROM control_plane.integration_grants grant_row
     WHERE grant_row.connection_id = @connection_id::uuid
       AND grant_row.enabled) AS active_grants
