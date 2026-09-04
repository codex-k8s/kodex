-- name: managed_configuration_lock_set :one
SELECT configuration.id::text, configuration.ref, COALESCE(project.id::text, ''), COALESCE(project.ref, ''), configuration.kind,
       configuration.name, configuration.managed_by, configuration.source, configuration.source_revision,
       configuration.version, configuration.updated_at, COALESCE(configuration.current_revision_id::text, '')
FROM control_plane.managed_configuration_sets configuration
LEFT JOIN control_plane.projects project ON project.id = configuration.project_id
WHERE configuration.organization_id = @organization_id::uuid AND configuration.ref = @configuration_ref
FOR UPDATE OF configuration;
