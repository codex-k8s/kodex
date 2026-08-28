-- name: runtime_configuration__lock_environment :one
SELECT environment.id::text,
       environment.project_id::text,
       project.ref,
       environment.version,
       current_version.id::text,
       current_version.version_number
FROM control_plane.runtime_environment_sets environment
JOIN control_plane.projects project ON project.id = environment.project_id
JOIN control_plane.runtime_environment_versions current_version ON current_version.id = environment.current_version_id
WHERE environment.organization_id = $1::uuid AND environment.ref = $2 AND environment.state = 'ACTIVE'
FOR UPDATE OF environment;
