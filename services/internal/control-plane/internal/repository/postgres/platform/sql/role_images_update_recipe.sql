-- name: role_images_update_recipe :exec
UPDATE control_plane.role_image_recipes
SET name = $3,
    specification = $4,
    generation = generation + 1,
    spec_sha256 = $5,
    policy_revision = $6,
    policy_sha256 = $7,
    role_runtime_contract_revision = $8,
    role_runtime_contract_sha256 = $9,
    active_image_artifact_id = NULL,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE organization_id = $1::uuid
  AND id = $2::uuid
