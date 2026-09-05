-- name: role_image_managed__configuration :one
SELECT configuration.ref
FROM control_plane.managed_role_image_recipes mapping
JOIN control_plane.role_image_recipes recipe ON recipe.id = mapping.recipe_id AND recipe.organization_id = mapping.organization_id
JOIN control_plane.managed_configuration_sets configuration ON configuration.id = mapping.configuration_set_id AND configuration.organization_id = mapping.organization_id
WHERE mapping.organization_id = $1::uuid AND recipe.ref = $2;
