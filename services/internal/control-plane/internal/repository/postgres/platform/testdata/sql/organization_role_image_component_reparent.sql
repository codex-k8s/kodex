-- name: organization_role_image_component_reparent :exec
UPDATE control_plane.image_builds SET recipe_id=(SELECT id FROM control_plane.role_image_recipes WHERE ref=$2)
WHERE ref=$1;
