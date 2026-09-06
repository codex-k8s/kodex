-- name: role_image_source__configuration_target :one
SELECT COALESCE(recipe.ref,''),COALESCE(project.ref,'')
FROM control_plane.managed_configuration_sets configuration
LEFT JOIN control_plane.projects project ON project.id=configuration.project_id AND project.organization_id=configuration.organization_id
LEFT JOIN control_plane.managed_role_image_recipes mapping ON mapping.configuration_set_id=configuration.id AND mapping.organization_id=configuration.organization_id
LEFT JOIN control_plane.role_image_recipes recipe ON recipe.id=mapping.recipe_id AND recipe.organization_id=configuration.organization_id
WHERE configuration.organization_id=@organization_id::uuid AND configuration.ref=@configuration_ref AND configuration.kind='ROLE_IMAGE';
