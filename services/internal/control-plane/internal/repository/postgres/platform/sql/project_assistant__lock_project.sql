-- name: project_assistant__lock_project :one
SELECT id::text
FROM control_plane.projects
WHERE organization_id = @organization_id::uuid AND ref = @project_ref AND lifecycle = 'ACTIVE'
  AND (@authority_project = '' OR id = NULLIF(@authority_project, '')::uuid)
FOR UPDATE;
