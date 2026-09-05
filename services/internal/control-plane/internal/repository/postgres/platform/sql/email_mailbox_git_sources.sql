-- name: email_mailbox_git_sources :many
SELECT source.mailbox_key,configuration.ref,configuration.managed_by,connection.ref,connection.id::text,connection.version
FROM control_plane.email_mailbox_git_sources source
JOIN control_plane.managed_configuration_sets configuration ON configuration.id=source.configuration_set_id
JOIN control_plane.email_mailbox_configuration_sets owner ON owner.configuration_set_id=configuration.id
JOIN control_plane.integration_connections connection ON connection.id=owner.connection_id
WHERE source.source=$1 AND configuration.organization_id=$2::uuid
ORDER BY connection.ref FOR UPDATE OF configuration,connection;
