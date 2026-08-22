-- name: platform__role_images_complete_promotion :one
UPDATE control_plane.image_artifacts
SET promotion_state = 'PROMOTED',
    promoted_reference = $4,
    promotion_readback_sha256 = $5,
    promoted_at = clock_timestamp(),
    promotion_claimant_workload = NULL,
    promotion_authority_generation = 0,
    promotion_authorization_token_sha256 = NULL,
    promotion_authorization_expires_at = NULL,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE organization_id = $1::uuid
  AND id = $2::uuid
  AND version = $3
RETURNING version, promoted_reference, promotion_readback_sha256, promoted_at, updated_at
