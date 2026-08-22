-- name: role_images_authorize_promotion :one
UPDATE control_plane.image_artifacts
SET promotion_state = 'AUTHORIZED',
    promotion_authorization_token_sha256 = $4,
    promotion_authorization_expires_at = $5,
    promotion_claim_token_sha256 = NULL,
    promotion_claim_expires_at = NULL,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE organization_id = $1::uuid
  AND id = $2::uuid
  AND version = $3
RETURNING version, updated_at
