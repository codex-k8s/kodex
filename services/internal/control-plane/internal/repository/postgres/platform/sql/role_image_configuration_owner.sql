-- name: role_image_configuration_owner :one
SELECT recipe.ref,recipe.scope_kind,organization.ref,COALESCE(project.ref,'')
FROM control_plane.managed_configuration_sets configuration
JOIN control_plane.managed_role_image_recipes mapping ON mapping.organization_id=configuration.organization_id
 AND mapping.configuration_set_id=configuration.id
JOIN control_plane.role_image_recipes recipe ON recipe.id=mapping.recipe_id AND recipe.organization_id=configuration.organization_id
 AND recipe.project_id IS NOT DISTINCT FROM configuration.project_id
JOIN control_plane.organizations organization ON organization.id=recipe.organization_id
LEFT JOIN control_plane.projects project ON project.id=recipe.project_id AND project.organization_id=recipe.organization_id
WHERE configuration.organization_id=$1::uuid AND configuration.ref=$2 AND configuration.kind='ROLE_IMAGE'
 AND ((recipe.scope_kind='ORGANIZATION' AND recipe.project_id IS NULL)
      OR (recipe.scope_kind='PROJECT' AND project.lifecycle='ACTIVE'));
