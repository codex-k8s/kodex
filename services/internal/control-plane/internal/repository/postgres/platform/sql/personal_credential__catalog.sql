-- name: personal_credential__catalog :many
SELECT connection.ref, connection.name, connection.definition_key, connection.version,
       connection.enabled, connection.credential_policy,
       (definition.enabled AND definition.adapter_owner='integration-gateway'
        AND definition.execution_route='MANAGED_MCP' AND (COALESCE(definition.credential_secret_key,'')<>'' OR definition.stable_key IN('kubernetes-managed','ssh-managed','ssh-execute'))) AS available,
       connection.definition_version, connection.definition_digest,
       COALESCE(package_source.content_format,''), COALESCE(package_source.content,''),
       COALESCE(binding.ref,''), COALESCE(binding.organization_id::text,''), COALESCE(binding.subject_id::text,''),
       connection.ref, connection.credential_policy, connection.version,
       COALESCE(binding.version,0), COALESCE(binding.generation,0), COALESCE(binding.state,''),
       COALESCE(revision.ref,''), COALESCE(revision.revision,0), COALESCE(revision.secret_ref,''),
       COALESCE(revision.secret_uid::text,''), COALESCE(revision.secret_resource_version,''),
       COALESCE(revision.content_sha256,''), revision.created_at,
       COALESCE(verified.ref,''), COALESCE(binding.verified_definition_digest,''),
       COALESCE(binding.verified_configuration_digest,''),
       COALESCE(binding.verified_capabilities,'{}'::text[]), COALESCE(binding.denied_capabilities,'{}'::text[]), COALESCE(binding.observed_permission_profiles,'{}'::text[]), binding.expires_at, COALESCE(binding.github_discovery,'null'::jsonb),
       COALESCE(binding.credential_kind,''), COALESCE(binding.broker_purpose,''),
       COALESCE(broker_revision.ref,''), COALESCE(broker_revision.revision,0),
       COALESCE(broker_revision.purpose,''), COALESCE(broker_revision.target_ref,''), broker_revision.created_at,
       COALESCE(broker_verified.ref,'')
FROM control_plane.integration_connections connection
JOIN control_plane.subjects subject ON subject.id=@subject_id::uuid
 AND subject.organization_id=connection.organization_id AND subject.kind='USER' AND subject.active
JOIN control_plane.integration_definitions definition ON definition.stable_key=connection.definition_key
LEFT JOIN LATERAL (
 SELECT package_revision.content_format, package_revision.content
 FROM control_plane.managed_configuration_bindings package_binding
 JOIN control_plane.managed_configuration_sets package_configuration
   ON package_configuration.id=package_binding.configuration_set_id
  AND package_configuration.organization_id=package_binding.organization_id
 JOIN control_plane.managed_configuration_revisions package_revision
   ON package_revision.id=package_binding.configuration_revision_id
  AND package_revision.configuration_set_id=package_configuration.id
  AND package_revision.organization_id=package_binding.organization_id
 WHERE package_binding.organization_id=connection.organization_id
   AND package_binding.consumer_kind='INTEGRATION_CONNECTION'
   AND package_binding.consumer_ref=connection.ref
   AND package_binding.configuration_kind='INTEGRATION_DEFINITION'
   AND package_configuration.kind='INTEGRATION_DEFINITION' AND package_revision.state='PUBLISHED'
) package_source ON true
LEFT JOIN control_plane.integration_personal_credentials binding
 ON binding.connection_id=connection.id AND binding.organization_id=connection.organization_id AND binding.subject_id=subject.id
LEFT JOIN control_plane.integration_personal_credential_revisions revision
 ON revision.id=binding.credential_revision_id AND revision.binding_id=binding.id AND revision.organization_id=binding.organization_id
LEFT JOIN control_plane.integration_personal_credential_revisions verified
 ON verified.id=binding.verified_credential_revision_id AND verified.binding_id=binding.id AND verified.organization_id=binding.organization_id
LEFT JOIN control_plane.integration_broker_identity_revisions broker_revision
 ON broker_revision.id=binding.broker_identity_revision_id AND broker_revision.binding_id=binding.id
 AND broker_revision.organization_id=binding.organization_id AND broker_revision.purpose=binding.broker_purpose
LEFT JOIN control_plane.integration_broker_identity_revisions broker_verified
 ON broker_verified.id=binding.verified_broker_identity_revision_id AND broker_verified.binding_id=binding.id
 AND broker_verified.organization_id=binding.organization_id AND broker_verified.purpose=binding.broker_purpose
WHERE connection.organization_id=@organization_id::uuid AND connection.lifecycle_state='ACTIVE'
 AND (binding.id IS NOT NULL OR (definition.enabled AND definition.adapter_owner='integration-gateway'
      AND definition.execution_route='MANAGED_MCP' AND (COALESCE(definition.credential_secret_key,'')<>'' OR definition.stable_key IN('kubernetes-managed','ssh-managed','ssh-execute'))))
 AND connection.ref>@after AND (@query='' OR strpos(lower(connection.name||' '||connection.definition_key||' '||connection.ref),lower(@query))>0)
 AND control_plane.catalog_resource_visible(connection.organization_id,subject.id,
     'integration.view','INTEGRATION',connection.id,NULL,connection.created_by,'{}'::jsonb,statement_timestamp())
ORDER BY connection.ref
LIMIT @limit
