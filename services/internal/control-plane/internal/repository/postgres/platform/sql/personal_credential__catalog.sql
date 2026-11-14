-- name: personal_credential__catalog :many
SELECT connection.ref, connection.name, connection.definition_key, connection.version,
       connection.enabled, connection.credential_policy,
       (definition.enabled AND definition.adapter_owner='integration-gateway'
        AND definition.execution_route='MANAGED_MCP' AND COALESCE(definition.credential_secret_key,'')<>'') AS available,
       COALESCE(binding.ref,''), COALESCE(binding.organization_id::text,''), COALESCE(binding.subject_id::text,''),
       connection.ref, connection.credential_policy, connection.version,
       COALESCE(binding.version,0), COALESCE(binding.generation,0), COALESCE(binding.state,''),
       COALESCE(revision.ref,''), COALESCE(revision.revision,0), COALESCE(revision.secret_ref,''),
       COALESCE(revision.secret_uid::text,''), COALESCE(revision.secret_resource_version,''),
       COALESCE(revision.content_sha256,''), revision.created_at,
       COALESCE(verified.ref,''), COALESCE(binding.verified_definition_digest,''),
       COALESCE(binding.verified_configuration_digest,''),
       COALESCE(binding.verified_capabilities,'{}'::text[]), COALESCE(binding.denied_capabilities,'{}'::text[]), COALESCE(binding.observed_permission_profiles,'{}'::text[]), binding.expires_at, COALESCE(binding.github_discovery,'null'::jsonb)
FROM control_plane.integration_connections connection
JOIN control_plane.subjects subject ON subject.id=@subject_id::uuid
 AND subject.organization_id=connection.organization_id AND subject.kind='USER' AND subject.active
JOIN control_plane.integration_definitions definition ON definition.stable_key=connection.definition_key
LEFT JOIN control_plane.integration_personal_credentials binding
 ON binding.connection_id=connection.id AND binding.organization_id=connection.organization_id AND binding.subject_id=subject.id
LEFT JOIN control_plane.integration_personal_credential_revisions revision
 ON revision.id=binding.credential_revision_id AND revision.binding_id=binding.id AND revision.organization_id=binding.organization_id
LEFT JOIN control_plane.integration_personal_credential_revisions verified
 ON verified.id=binding.verified_credential_revision_id AND verified.binding_id=binding.id AND verified.organization_id=binding.organization_id
WHERE connection.organization_id=@organization_id::uuid AND connection.lifecycle_state='ACTIVE'
 AND (binding.id IS NOT NULL OR (definition.enabled AND definition.adapter_owner='integration-gateway'
      AND definition.execution_route='MANAGED_MCP' AND COALESCE(definition.credential_secret_key,'')<>''))
 AND connection.ref>@after AND (@query='' OR strpos(lower(connection.name||' '||connection.definition_key||' '||connection.ref),lower(@query))>0)
 AND control_plane.catalog_resource_visible(connection.organization_id,subject.id,
     'integration.view','INTEGRATION',connection.id,NULL,connection.created_by,'{}'::jsonb,statement_timestamp())
ORDER BY connection.ref
LIMIT @limit
