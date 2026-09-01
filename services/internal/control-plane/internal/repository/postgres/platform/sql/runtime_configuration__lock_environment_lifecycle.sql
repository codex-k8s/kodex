-- name: runtime_configuration__lock_environment_lifecycle :one
SELECT environment.id::text, environment.project_id::text, project.ref,
       environment.state, environment.version
FROM control_plane.runtime_environment_sets environment
JOIN control_plane.projects project ON project.id = environment.project_id
WHERE environment.organization_id = @organization_id::uuid
  AND environment.ref = @environment_ref
  AND environment.state <> 'DELETED'
FOR UPDATE OF environment;
