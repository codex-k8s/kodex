-- name: runtime_configuration__list_environments :many
SELECT environment.ref,
       environment.version,
       project.ref,
       environment.name,
       environment.description,
       environment.state,
       environment.updated_at,
       current_version.ref,
       current_version.version_number,
       current_version.non_secret_values,
       current_version.secret_descriptors,
       current_version.digest,
       current_version.created_at
FROM control_plane.runtime_environment_sets environment
JOIN control_plane.projects project ON project.id = environment.project_id
JOIN control_plane.runtime_environment_versions current_version ON current_version.id = environment.current_version_id
WHERE environment.organization_id = @organization_id::uuid
  AND project.ref = @project_ref
  AND environment.state = 'ACTIVE'
  AND (@query = '' OR environment.name ILIKE '%' || @query || '%' OR environment.description ILIKE '%' || @query || '%')
  AND (@cursor_ref = '' OR (lower(environment.name), environment.ref) > (
      SELECT lower(cursor.name), cursor.ref
      FROM control_plane.runtime_environment_sets cursor
      WHERE cursor.organization_id = @organization_id::uuid AND cursor.ref = @cursor_ref
  ))
  AND (@platform_role IN ('OWNER', 'ADMINISTRATOR') OR EXISTS (
      SELECT 1 FROM control_plane.memberships membership
      WHERE membership.project_id = environment.project_id
        AND membership.subject_id = @actor_id::uuid
        AND membership.active
        AND 'VIEW' = ANY(membership.permissions)
  ))
ORDER BY lower(environment.name), environment.ref
LIMIT @page_size;
