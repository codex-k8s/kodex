-- name: email_mailbox_git_connection_claimed :one
SELECT EXISTS (
    SELECT 1 FROM control_plane.email_mailbox_configuration_sets
    WHERE organization_id=$1::uuid AND connection_id=$2::uuid
);
