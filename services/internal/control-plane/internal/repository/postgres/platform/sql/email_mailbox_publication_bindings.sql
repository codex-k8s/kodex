-- name: email_mailbox_publication_bindings :many
SELECT effect.organization_id::text,connection.ref,effect.connection_id::text,effect.connection_version,connection.version,
    COALESCE(effect.configuration_set_id::text,''),COALESCE(effect.revision_id::text,''),connection.enabled AND connection.state<>'DELETED'
FROM control_plane.email_mailbox_publication_bindings effect
JOIN control_plane.integration_connections connection ON connection.id=effect.connection_id AND connection.organization_id=effect.organization_id
WHERE effect.publication_ref=$1 ORDER BY connection.ref FOR UPDATE OF connection;
