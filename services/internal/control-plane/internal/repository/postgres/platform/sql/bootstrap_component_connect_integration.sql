-- name: platform__bootstrap_component_connect_integration :one
UPDATE control_plane.integration_connections SET state='CONNECTED',masked_credentials_state='CONFIGURED',last_test_summary='i18n:INTEGRATION_TEST_SUCCEEDED',last_tested_at=clock_timestamp(),version=version+1,updated_at=clock_timestamp() WHERE ref=$1 RETURNING version
