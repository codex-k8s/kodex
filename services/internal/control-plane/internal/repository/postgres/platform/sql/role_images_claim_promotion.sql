-- name: role_images_claim_promotion :one
UPDATE control_plane.image_artifacts
SET promotion_state = 'CLAIMED',
    promotion_claimant_workload = $4,
    promotion_authority_generation = $5,
    promotion_fence = $6,
    promotion_claim_token_sha256 = $7,
    promotion_claim_expires_at = $8,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE organization_id = $1::uuid
  AND id = $2::uuid
  AND version = $3
RETURNING id::text
