-- name: role_images_cancel_open_promotions :exec
WITH failed_requests AS (
    UPDATE control_plane.role_image_promotion_requests
    SET state = 'FAILED',
        updated_at = clock_timestamp()
    WHERE organization_id = $1::uuid
      AND recipe_id = $2::uuid
      AND state IN ('QUEUED', 'PROMOTING')
    RETURNING image_artifact_id
)
UPDATE control_plane.image_artifacts artifact
SET promotion_state = 'REJECTED',
    promotion_claimant_workload = NULL,
    promotion_authority_generation = 0,
    promotion_claim_token_sha256 = NULL,
    promotion_claim_expires_at = NULL,
    promotion_authorization_token_sha256 = NULL,
    promotion_authorization_expires_at = NULL,
    version = version + 1,
    updated_at = clock_timestamp()
FROM failed_requests request
WHERE artifact.organization_id = $1::uuid
  AND artifact.recipe_id = $2::uuid
  AND artifact.id = request.image_artifact_id
  AND artifact.promotion_state IN ('PENDING', 'CLAIMED', 'AUTHORIZED');
