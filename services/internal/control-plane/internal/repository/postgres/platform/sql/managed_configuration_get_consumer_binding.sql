-- name: managed_configuration_get_consumer_binding :one
SELECT binding.ref, binding.version, binding.consumer_kind, binding.consumer_ref,
       configuration.id::text, configuration.ref, COALESCE(configuration.project_id::text, ''),
       COALESCE(project.ref, ''), configuration.kind, configuration.name, configuration.managed_by,
       configuration.source, configuration.source_revision, configuration.version,
       configuration.updated_at, COALESCE(configuration.current_revision_id::text, ''),
       revision.id::text, revision.ref, revision.revision, revision.state,
       revision.content_format, revision.content, revision.digest,
       COALESCE(parent.ref, ''), revision.validation_diagnostics,
       revision.created_at, revision.validated_at, revision.published_at
FROM control_plane.managed_configuration_bindings binding
JOIN control_plane.managed_configuration_sets configuration
  ON configuration.id = binding.configuration_set_id
 AND configuration.organization_id = binding.organization_id
JOIN control_plane.managed_configuration_revisions revision
  ON revision.id = binding.configuration_revision_id
 AND revision.configuration_set_id = configuration.id
 AND revision.organization_id = binding.organization_id
LEFT JOIN control_plane.managed_configuration_revisions parent ON parent.id = revision.parent_revision_id
LEFT JOIN control_plane.projects project ON project.id = configuration.project_id
WHERE binding.organization_id = @organization_id::uuid
  AND binding.consumer_kind = @consumer_kind
  AND binding.consumer_ref = @consumer_ref
  AND binding.configuration_kind = @configuration_kind
  AND configuration.kind = @configuration_kind
  AND revision.state IN ('PUBLISHED', 'SUPERSEDED')
  AND CASE @consumer_kind
      WHEN 'RUNTIME_ENVIRONMENT' THEN EXISTS (
          SELECT 1 FROM control_plane.runtime_environment_sets environment
          WHERE environment.organization_id = binding.organization_id
            AND environment.ref = binding.consumer_ref
            AND environment.state <> 'DELETED'
            AND environment.project_id IS NOT DISTINCT FROM configuration.project_id
      )
      WHEN 'INTEGRATION_CONNECTION' THEN EXISTS (
          SELECT 1 FROM control_plane.integration_connections connection
          WHERE connection.organization_id = binding.organization_id
            AND connection.ref = binding.consumer_ref
            AND connection.lifecycle_state = 'ACTIVE'
      )
      ELSE false
  END
ORDER BY configuration.ref
LIMIT 1;
