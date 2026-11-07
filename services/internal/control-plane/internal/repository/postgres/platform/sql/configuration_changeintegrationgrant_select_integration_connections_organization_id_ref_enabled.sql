-- name: configuration_changeintegrationgrant_select_integration_connections_organization_id_ref_enabled :one
SELECT id::text,definition_key,enabled,state,version,public_configuration,definition_version,definition_digest,
       control_plane.integration_connection_configuration_ready(id)
FROM control_plane.integration_connections
WHERE organization_id=$1::uuid AND ref=$2 AND lifecycle_state='ACTIVE'
FOR UPDATE
