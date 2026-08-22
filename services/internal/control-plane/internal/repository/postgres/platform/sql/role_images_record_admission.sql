-- name: platform__role_images_record_admission :one
UPDATE control_plane.image_artifacts
SET admission_state = $4,
    sbom_sha256 = $5,
    vulnerability_evidence_sha256 = $6,
    admission_verdict = $4,
    signature_identity = $7,
    signature_sha256 = $8,
    admission_revision = admission_revision + 1,
    admission_receipt_sha256 = $9,
    admission_receipt_oci_manifest_digest = $10,
    admission_claimant_workload = NULL,
    admission_authority_generation = 0,
    admission_claim_token_sha256 = NULL,
    admission_claim_expires_at = NULL,
    promotion_state = CASE WHEN $4 = 'ACCEPTED' THEN 'PENDING' ELSE 'REJECTED' END,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE organization_id = $1::uuid
  AND id = $2::uuid
  AND version = $3
RETURNING version, admission_verdict, admission_revision, updated_at
