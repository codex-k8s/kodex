-- name: email_credential_advance_connection :one
UPDATE control_plane.integration_connections SET version=version+1,updated_at=clock_timestamp()
WHERE id=$1::uuid AND organization_id=$2::uuid AND version=$3 RETURNING version;
