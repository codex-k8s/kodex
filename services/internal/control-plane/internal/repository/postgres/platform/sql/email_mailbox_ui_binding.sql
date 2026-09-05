-- name: email_mailbox_ui_binding :one
SELECT EXISTS (
    SELECT 1 FROM control_plane.email_mailbox_configuration_bindings binding
    JOIN control_plane.managed_configuration_sets configuration ON configuration.id=binding.configuration_set_id
    JOIN control_plane.integration_connections connection ON connection.id=binding.connection_id
    WHERE binding.organization_id=$1::uuid AND connection.ref=$2 AND configuration.managed_by='UI'
);
