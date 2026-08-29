-- name: runtime_secret_lock_project :one
SELECT project.id::text, project.ref
FROM control_plane.projects project
WHERE project.organization_id = @organization_id::uuid
  AND project.ref = @project_ref
  AND project.lifecycle = 'ACTIVE'
FOR UPDATE;
