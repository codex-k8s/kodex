-- name: managed_configuration_access_target :one
SELECT COALESCE(project.ref, '')
FROM control_plane.managed_configuration_sets configuration
LEFT JOIN control_plane.projects project ON project.id = configuration.project_id
WHERE configuration.organization_id = @organization_id::uuid
  AND configuration.ref = @configuration_ref;
