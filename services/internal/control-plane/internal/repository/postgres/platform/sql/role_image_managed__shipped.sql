-- name: role_image_managed__shipped :one
SELECT EXISTS (
    SELECT 1 FROM control_plane.managed_role_image_recipes mapping
    JOIN control_plane.role_image_recipes recipe ON recipe.id = mapping.recipe_id AND recipe.organization_id = mapping.organization_id
    WHERE mapping.configuration_set_id = $1::uuid AND mapping.organization_id = $2::uuid
      AND recipe.specification->>'SourceRef' = $3 AND recipe.specification->>'EnvironmentKey' = 'system-base'
);
