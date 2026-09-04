-- name: managed_configuration_list :many
SELECT configuration.ref, COALESCE(project.ref, ''), configuration.kind,
       configuration.name, configuration.managed_by, configuration.source,
       configuration.source_revision, configuration.version, configuration.updated_at,
       COALESCE(revision.ref, ''), COALESCE(revision.revision, 0),
       COALESCE(revision.state, ''), COALESCE(revision.digest, '')
FROM control_plane.managed_configuration_sets configuration
LEFT JOIN control_plane.projects project ON project.id = configuration.project_id
LEFT JOIN control_plane.managed_configuration_revisions revision
  ON revision.id = configuration.current_revision_id
 AND revision.configuration_set_id = configuration.id
WHERE configuration.organization_id = @organization_id::uuid
  AND (@project_ref = '' OR project.ref = @project_ref)
  AND (@kind = '' OR configuration.kind = @kind)
  AND (@query = '' OR configuration.name ILIKE '%' || @query || '%')
  AND configuration.ref > @cursor_ref
ORDER BY configuration.ref
LIMIT @page_size;
