-- name: role_images_cancel_open_builds :exec
UPDATE control_plane.image_builds
SET stage = 'CANCELLED',
    safe_error_code = 'IMAGE_BUILD_SUPERSEDED',
    claimant_workload = NULL,
    authority_generation = 0,
    lease_token_sha256 = NULL,
    lease_expires_at = NULL,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE organization_id = $1::uuid
  AND recipe_id = $2::uuid
  AND stage NOT IN ('COMPLETED', 'CANCELLED', 'DEAD_LETTER')
