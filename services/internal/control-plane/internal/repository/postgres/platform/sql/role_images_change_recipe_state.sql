-- name: role_images_change_recipe_state :exec
UPDATE control_plane.role_image_recipes
SET state = $3,
    generation = CASE WHEN $3 = 'ACTIVE' THEN generation + 1 ELSE generation END,
    active_image_artifact_id = CASE WHEN $3 = 'ACTIVE' THEN NULL ELSE active_image_artifact_id END,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE organization_id = $1::uuid
  AND id = $2::uuid
