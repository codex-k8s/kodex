-- name: image_admission_failure_readback :one
SELECT admission_state, admission_verdict, admission_failure_code,
       admission_claim_token_sha256 IS NULL, admission_claim_expires_at IS NULL,
       admission_authority_generation, sbom_sha256, admission_revision
FROM control_plane.image_artifacts WHERE organization_id = $1::uuid AND ref = $2;
