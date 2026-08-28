-- name: runtime_configuration__get_environment :one
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
WHERE environment.organization_id = $1::uuid
  AND environment.ref = $2
  AND ($3 IN ('OWNER', 'ADMINISTRATOR') OR EXISTS (
      SELECT 1 FROM control_plane.memberships membership
      WHERE membership.project_id = environment.project_id
        AND membership.subject_id = $4::uuid
        AND membership.active
        AND 'VIEW' = ANY(membership.permissions)
  ));
