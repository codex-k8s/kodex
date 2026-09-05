-- name: email_mailbox_configuration_insert_owner :exec
INSERT INTO control_plane.email_mailbox_configuration_sets
    (configuration_set_id,organization_id,connection_id,mailbox_ref)
SELECT configuration.id,configuration.organization_id,connection.id,$4
FROM control_plane.managed_configuration_sets configuration
JOIN control_plane.integration_connections connection ON connection.organization_id=configuration.organization_id
    AND connection.ref=$3 AND connection.definition_key='email' AND connection.state<>'DELETED'
WHERE configuration.organization_id=$1::uuid AND configuration.ref=$2 AND configuration.kind='EMAIL_MAILBOX';
