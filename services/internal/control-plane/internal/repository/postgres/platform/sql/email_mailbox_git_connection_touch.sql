-- name: email_mailbox_git_connection_touch :one
UPDATE control_plane.integration_connections SET version=version+1,updated_at=clock_timestamp()
WHERE organization_id=$1::uuid AND id=$2::uuid AND version=$3
RETURNING version;
