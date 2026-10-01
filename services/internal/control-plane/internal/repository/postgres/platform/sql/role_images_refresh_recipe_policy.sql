-- name: role_images_refresh_recipe_policy :exec
UPDATE control_plane.role_image_recipes
SET policy_revision = $3,
    policy_sha256 = $4,
    role_runtime_contract_revision = $5,
    role_runtime_contract_sha256 = $6,
    active_image_artifact_id = NULL,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE organization_id = $1::uuid
  AND id = $2::uuid
