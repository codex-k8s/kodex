-- name: integration_credential_policy__set :one
UPDATE control_plane.integration_connections
SET credential_policy=@policy,
    version=version+1,
    last_test_summary='',last_tested_at=NULL,
    updated_at=clock_timestamp()
WHERE id=@connection_id::uuid AND version=@expected_version
  AND lifecycle_state='ACTIVE' AND NOT enabled AND state='DISABLED'
RETURNING ref
