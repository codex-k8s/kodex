-- name: role_images_resolve_access_target :one
SELECT recipe.id::text, project.id::text, project.ref, owner_subject.ref
FROM control_plane.role_image_recipes recipe
JOIN control_plane.projects project ON project.id = recipe.project_id
JOIN control_plane.subjects owner_subject ON owner_subject.id = recipe.created_by
WHERE recipe.organization_id = $1::uuid
  AND recipe.ref = $2
  AND project.lifecycle = 'ACTIVE'
