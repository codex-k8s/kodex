-- name: configuration_changeconnection_activate_credential_revision :exec
UPDATE control_plane.integration_connections
SET credential_revision_id=$2::uuid
WHERE id=$1::uuid AND credential_revision_id IS NULL
