-- name: managed_configuration_insert_set :one
INSERT INTO control_plane.managed_configuration_sets
    (ref, organization_id, project_id, kind, name, managed_by, source, source_revision, created_by)
SELECT @configuration_ref, @organization_id::uuid, project.id, @kind, @name, @managed_by, @source, @source_revision, @actor_id::uuid
FROM (SELECT 1) singleton
LEFT JOIN control_plane.projects project
  ON project.organization_id = @organization_id::uuid AND project.ref = @project_ref AND project.lifecycle <> 'ARCHIVED'
WHERE (@kind IN ('SYSTEM_STT', 'INTEGRATION_DEFINITION') AND @project_ref = '')
   OR (@kind NOT IN ('SYSTEM_STT', 'INTEGRATION_DEFINITION') AND project.id IS NOT NULL)
RETURNING id::text, ref, COALESCE(project_id::text, ''), COALESCE((SELECT ref FROM control_plane.projects WHERE id = project_id), ''),
          kind, name, managed_by, source, source_revision, version, updated_at, '';
