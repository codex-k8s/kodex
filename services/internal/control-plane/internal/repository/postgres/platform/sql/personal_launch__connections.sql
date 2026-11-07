-- name: personal_launch__connections :many
SELECT connection.ref,connection.name,connection.version,connection.credential_policy,
       connection.definition_key,connection.definition_version,connection.definition_digest,
       connection.public_configuration,
       control_plane.integration_connection_configuration_ready(connection.id),
       control_plane.catalog_resource_visible(connection.organization_id,@subject_id::uuid,
         'integration.view','INTEGRATION',connection.id,NULL,connection.created_by,'{}'::jsonb,statement_timestamp()),
       control_plane.catalog_resource_visible(connection.organization_id,@subject_id::uuid,
         'integration.manage','INTEGRATION',connection.id,NULL,connection.created_by,'{}'::jsonb,statement_timestamp()),
       COALESCE(subject.kind,'')
FROM control_plane.integration_connections connection
LEFT JOIN control_plane.subjects subject ON subject.organization_id=connection.organization_id
 AND subject.id=@subject_id::uuid AND subject.active
WHERE connection.organization_id=@organization_id::uuid AND connection.ref=ANY(@connection_refs::text[])
ORDER BY connection.ref
FOR UPDATE OF connection;
