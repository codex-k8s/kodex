-- name: email_mailbox_configuration_latest_revision :one
SELECT ref FROM control_plane.managed_configuration_revisions
WHERE organization_id=$1::uuid AND configuration_set_id=$2::uuid
ORDER BY revision DESC LIMIT 1;
