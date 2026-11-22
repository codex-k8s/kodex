-- name: configuration_changeconnection_count_active_dependencies :one
SELECT
    (SELECT count(*)
     FROM control_plane.integration_connection_tests test
     WHERE test.connection_id = @connection_id::uuid
       AND test.state IN ('DUE', 'CLAIMED'))
  + (SELECT count(*) FROM control_plane.integration_personal_credential_tests personal_test
     WHERE personal_test.connection_id=@connection_id::uuid AND personal_test.state IN ('DUE','CLAIMED','PROBING'))
  + (SELECT count(*) FROM control_plane.kubernetes_identity_setups setup
     WHERE setup.connection_id=@connection_id::uuid AND setup.state IN('PREPARED','ATTEMPTING','UNKNOWN'))
  + (SELECT count(*) FROM control_plane.kubernetes_identity_upload_attempts attempt
     JOIN control_plane.kubernetes_identity_setups setup ON setup.id=attempt.setup_id
      AND setup.organization_id=attempt.organization_id
     WHERE setup.connection_id=@connection_id::uuid AND attempt.state='ACTIVE')
  + (SELECT count(*)
     FROM control_plane.integration_invocations invocation
     WHERE invocation.connection_id = @connection_id::uuid
       AND invocation.state IN ('WAITING_APPROVAL', 'READY', 'RUNNING', 'UNKNOWN_OUTCOME'))
  + (SELECT count(*)
     FROM control_plane.interaction_deliveries delivery
     WHERE delivery.connection_id = @connection_id::uuid
       AND delivery.state IN ('WAITING_APPROVAL', 'DUE', 'CLAIMED', 'FAILED', 'UNKNOWN_OUTCOME')
       AND NOT (@allow_terminal_telegram_unknown::boolean AND delivery.capability_key='telegram.notifications'
        AND delivery.telegram_subscription_id IS NOT NULL AND delivery.state='UNKNOWN_OUTCOME' AND delivery.completed_at IS NOT NULL
        AND delivery.lease_ref IS NULL AND delivery.fence_digest IS NULL AND delivery.workload_instance IS NULL AND delivery.lease_expires_at IS NULL)) AS active_effects,
    (SELECT count(*)
     FROM control_plane.integration_grants grant_row
     WHERE grant_row.connection_id = @connection_id::uuid
       AND grant_row.enabled) AS active_grants
