-- name: system_assistant_grant__header :one
SELECT connection.ref,connection.name,connection.version,
       connection.definition_key,connection.definition_version,connection.definition_digest
FROM control_plane.integration_connections connection
WHERE connection.organization_id=@organization_id::uuid AND connection.ref=@connection_ref
  AND connection.lifecycle_state='ACTIVE' AND @authority_project=''
  AND control_plane.catalog_resource_visible(connection.organization_id,@actor_id::uuid,
      'organization.manage','ORGANIZATION',connection.organization_id,NULL,NULL,'{}'::jsonb,transaction_timestamp())
  AND control_plane.catalog_resource_visible(connection.organization_id,@actor_id::uuid,
      'integration.manage','INTEGRATION',connection.id,NULL,connection.created_by,'{}'::jsonb,transaction_timestamp());
