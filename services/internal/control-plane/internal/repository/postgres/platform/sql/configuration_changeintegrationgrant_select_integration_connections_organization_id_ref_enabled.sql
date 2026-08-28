-- name: configuration_changeintegrationgrant_select_integration_connections_organization_id_ref_enabled :one
SELECT id::text,definition_key,enabled,state,version,public_configuration,definition_version,definition_digest
FROM control_plane.integration_connections
WHERE organization_id=$1::uuid AND ref=$2
FOR UPDATE
