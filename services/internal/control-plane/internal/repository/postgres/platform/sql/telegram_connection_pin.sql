-- name: telegram_connection_pin :one
SELECT credential.id::text
FROM control_plane.integration_connections connection
JOIN control_plane.integration_credential_revisions credential ON credential.id=connection.credential_revision_id
 AND credential.organization_id=connection.organization_id AND credential.connection_id=connection.id
WHERE connection.organization_id=$1::uuid AND connection.id=$2::uuid AND connection.credential_policy='PROJECT_SHARED'
 AND connection.enabled AND connection.lifecycle_state='ACTIVE' AND connection.state='CONNECTED'
 AND control_plane.integration_connection_configuration_ready(connection.id)
FOR SHARE OF credential;
