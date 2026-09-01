-- name: role_images_reject_stale_admission_candidates :exec
WITH rejected_artifacts AS (
    UPDATE control_plane.image_artifacts artifact
    SET admission_state = 'REJECTED',
        admission_claimant_workload = NULL,
        admission_authority_generation = 0,
        admission_claim_token_sha256 = NULL,
        admission_claim_expires_at = NULL,
        promotion_state = 'REJECTED',
        promotion_claimant_workload = NULL,
        promotion_authority_generation = 0,
        promotion_claim_token_sha256 = NULL,
        promotion_claim_expires_at = NULL,
        promotion_authorization_token_sha256 = NULL,
        promotion_authorization_expires_at = NULL,
        version = version + 1,
        updated_at = clock_timestamp()
    WHERE artifact.organization_id = @organization_id::uuid
      AND artifact.admission_state IN ('PENDING', 'CLAIMED')
      AND (
          artifact.policy_revision <> @policy_revision
          OR artifact.policy_sha256 <> @policy_sha256
      )
    RETURNING artifact.id
)
UPDATE control_plane.role_image_promotion_requests request
SET state = 'FAILED',
    updated_at = clock_timestamp()
FROM rejected_artifacts artifact
WHERE request.organization_id = @organization_id::uuid
  AND request.image_artifact_id = artifact.id
  AND request.state IN ('QUEUED', 'PROMOTING');
