-- name: email_mailbox_binding_remove :exec
DELETE FROM control_plane.email_mailbox_configuration_bindings WHERE organization_id=$1::uuid AND connection_id=$2::uuid;
