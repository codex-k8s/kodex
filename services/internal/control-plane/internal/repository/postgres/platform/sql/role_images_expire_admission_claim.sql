-- name: role_images_expire_admission_claim :one
UPDATE control_plane.image_artifacts
SET admission_state = 'FAILED', admission_failure_code = @error_code,
    admission_failure_authority_generation = admission_authority_generation,
    admission_claim_token_sha256 = NULL, admission_claim_expires_at = NULL,
    admission_authority_generation = 0, admission_claimant_workload = NULL,
    version = version + 1, updated_at = clock_timestamp()
WHERE organization_id = @organization_id::uuid AND id = @artifact_id::uuid
  AND version = @expected_version AND admission_state = 'CLAIMED'
  AND admission_claimant_workload = 'image-admission'
  AND admission_claim_expires_at <= clock_timestamp()
RETURNING version
