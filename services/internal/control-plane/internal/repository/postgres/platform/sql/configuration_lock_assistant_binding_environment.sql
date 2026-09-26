-- name: configuration_lock_assistant_binding_environment :one
SELECT environment.ref
FROM control_plane.runtime_environment_sets environment
JOIN control_plane.projects project ON project.id = environment.project_id
WHERE environment.organization_id = $1::uuid
  AND project.ref = $2
  AND project.lifecycle = 'ACTIVE'
  AND environment.ref = $3
  AND environment.state = 'ACTIVE'
FOR SHARE OF environment;
