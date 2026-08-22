-- name: platform__role_images_renew_build :one
UPDATE control_plane.image_builds
SET lease_token_sha256 = $4,
    lease_expires_at = $5,
    authority_generation = $6,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE organization_id = $1::uuid
  AND id = $2::uuid
  AND version = $3
RETURNING version, lease_expires_at, updated_at
