-- name: assistant_recipient_integration_catalog__entries :many
SELECT connection.ref,admission.capability_key
FROM control_plane.integration_grant_admission(@organization_id::uuid,@actor_id::uuid,
 NULLIF(@authority_project_id,'')::uuid,'',@project_ref,@recipient_kind,@recipient_ref,'','GRANT',NULL) admission
JOIN control_plane.integration_connections connection ON connection.id=admission.connection_id
JOIN control_plane.integration_definitions definition ON definition.stable_key=admission.definition_key
WHERE admission.project_ref=@project_ref AND admission.recipient_kind=@recipient_kind AND admission.recipient_ref=@recipient_ref
 AND NOT (admission.definition_key='openapi-mcp' AND admission.definition_version=definition.definition_version AND admission.definition_digest=definition.digest)
 AND (
   (@shipped_revisions::jsonb -> connection.definition_key ->> 'version'=connection.definition_version
    AND @shipped_revisions::jsonb -> connection.definition_key ->> 'digest'=connection.definition_digest)
   OR EXISTS (
     SELECT 1 FROM control_plane.managed_configuration_bindings binding
     JOIN control_plane.managed_configuration_sets configuration
       ON configuration.id=binding.configuration_set_id AND configuration.organization_id=binding.organization_id
     JOIN control_plane.managed_configuration_revisions revision
       ON revision.id=binding.configuration_revision_id AND revision.configuration_set_id=configuration.id
      AND revision.organization_id=binding.organization_id
     WHERE binding.organization_id=@organization_id::uuid
       AND binding.consumer_kind='INTEGRATION_CONNECTION' AND binding.consumer_ref=connection.ref
       AND binding.configuration_kind='INTEGRATION_DEFINITION'
       AND configuration.kind='INTEGRATION_DEFINITION' AND revision.state='PUBLISHED'
   )
 )
 AND control_plane.catalog_resource_visible(@organization_id::uuid,@actor_id::uuid,'integration.view','INTEGRATION',connection.id,NULL,connection.created_by,'{}'::jsonb,transaction_timestamp())
 AND (@query='' OR strpos(lower(connection.name || ' ' || admission.capability_key || ' ' || admission.capability_name),lower(@query))>0)
ORDER BY connection.ref,admission.capability_key LIMIT 11 OFFSET @offset;
