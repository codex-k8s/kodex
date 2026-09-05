-- name: email_mailbox_configuration_binding :one
SELECT configuration.ref,revision.ref
FROM control_plane.email_mailbox_configuration_bindings binding
JOIN control_plane.managed_configuration_sets configuration ON configuration.id=binding.configuration_set_id
JOIN control_plane.managed_configuration_revisions revision ON revision.id=binding.revision_id AND revision.configuration_set_id=configuration.id
JOIN control_plane.integration_connections connection ON connection.id=binding.connection_id AND connection.organization_id=binding.organization_id
WHERE binding.organization_id=$1::uuid AND connection.ref=$2;
