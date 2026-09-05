-- name: role_image_managed__count :one
SELECT count(*)
FROM control_plane.role_image_recipes recipe
JOIN control_plane.projects project ON project.id = recipe.project_id AND project.organization_id = recipe.organization_id
JOIN control_plane.role_definitions role ON role.id = recipe.role_definition_id AND role.organization_id = recipe.organization_id
JOIN control_plane.subjects owner_subject ON owner_subject.id = recipe.created_by AND owner_subject.organization_id = recipe.organization_id
LEFT JOIN control_plane.image_artifacts artifact ON artifact.id = recipe.active_image_artifact_id
WHERE recipe.organization_id = @organization_id::uuid AND project.ref = @project_ref
  AND (@role_ref = '' OR role.ref = @role_ref)
  AND (@state = '' OR recipe.state = @state)
  AND (@query = '' OR strpos(lower(recipe.name), lower(@query)) > 0 OR strpos(lower(recipe.ref), lower(@query)) > 0)
  AND (@authority_project = '' OR recipe.project_id = NULLIF(@authority_project, '')::uuid)
  AND control_plane.catalog_resource_visible(recipe.organization_id, @actor_id::uuid, 'project.view',
      'ROLE_IMAGE', recipe.id, recipe.project_id, recipe.created_by, jsonb_build_object('PROJECT', recipe.project_id::text), transaction_timestamp());
