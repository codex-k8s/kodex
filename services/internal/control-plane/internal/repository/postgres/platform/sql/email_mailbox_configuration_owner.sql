-- name: email_mailbox_configuration_owner :one
SELECT connection.ref,owner.mailbox_ref
FROM control_plane.email_mailbox_configuration_sets owner
JOIN control_plane.managed_configuration_sets configuration ON configuration.id=owner.configuration_set_id
    AND configuration.organization_id=owner.organization_id AND configuration.kind='EMAIL_MAILBOX'
JOIN control_plane.integration_connections connection ON connection.id=owner.connection_id
    AND connection.organization_id=owner.organization_id AND connection.definition_key='email' AND connection.state<>'DELETED'
WHERE owner.organization_id=$1::uuid AND configuration.ref=$2;
