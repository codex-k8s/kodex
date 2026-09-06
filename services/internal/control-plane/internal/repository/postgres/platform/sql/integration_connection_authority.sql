-- name: integration_connection_authority :one
SELECT control_plane.catalog_resource_visible(connection.organization_id,$2::uuid,
 'integration.manage','INTEGRATION',connection.id,NULL,connection.created_by,'{}'::jsonb,transaction_timestamp())
FROM control_plane.integration_connections connection
WHERE connection.organization_id=$1::uuid AND connection.ref=$3 AND connection.lifecycle_state='ACTIVE'
