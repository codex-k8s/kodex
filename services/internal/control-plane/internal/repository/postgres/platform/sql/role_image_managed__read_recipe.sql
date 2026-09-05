-- name: role_image_managed__read_recipe :one
SELECT recipe.ref,recipe.version,role.ref FROM control_plane.managed_role_image_recipes mapping
JOIN control_plane.role_image_recipes recipe ON recipe.id=mapping.recipe_id AND recipe.organization_id=mapping.organization_id
JOIN control_plane.role_definitions role ON role.id=recipe.role_definition_id AND role.organization_id=recipe.organization_id
WHERE mapping.configuration_set_id=$1::uuid AND mapping.organization_id=$2::uuid FOR UPDATE OF recipe;
