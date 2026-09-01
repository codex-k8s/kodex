-- name: configuration_changeconnection_activate_credential_revision :one
UPDATE control_plane.integration_connections
SET credential_revision_id=$2::uuid,
    credential_materialization_ref=$3,
    masked_credentials_state='CONFIGURED',
    state='NOT_CONNECTED',
    last_test_summary='',
    version=version+1,
    updated_at=clock_timestamp()
WHERE id=$1::uuid AND version=$4
RETURNING ref
