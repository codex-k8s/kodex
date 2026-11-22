-- name: kubernetes_prepared__project_lock :one
SELECT project.id::text FROM control_plane.projects project
WHERE project.id=@project_id::uuid AND project.organization_id=@organization_id::uuid
 AND project.lifecycle='ACTIVE'
FOR SHARE OF project;
