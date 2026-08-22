-- name: platform__role_images_claim_admission :one
UPDATE control_plane.image_artifacts
SET admission_state = 'CLAIMED',
    admission_claimant_workload = $4,
    admission_authority_generation = $5,
    admission_fence = $6,
    admission_claim_token_sha256 = $7,
    admission_claim_expires_at = $8,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE organization_id = $1::uuid
  AND id = $2::uuid
  AND version = $3
RETURNING id::text
