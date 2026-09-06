-- name: configuration_source__credential :one
SELECT connection.id::text,credential.id::text
FROM control_plane.integration_connections connection
JOIN control_plane.integration_definitions definition ON definition.stable_key=connection.definition_key AND definition.enabled
JOIN control_plane.integration_credential_revisions credential ON credential.id=connection.credential_revision_id
 AND credential.connection_id=connection.id AND credential.organization_id=connection.organization_id
WHERE connection.organization_id=$1::uuid AND connection.ref=$2 AND credential.ref=$3
 AND connection.lifecycle_state='ACTIVE' AND connection.enabled AND connection.state='CONNECTED';
