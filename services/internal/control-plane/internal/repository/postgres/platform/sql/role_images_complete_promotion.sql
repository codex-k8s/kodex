-- name: role_images_complete_promotion :one
UPDATE control_plane.image_artifacts
SET promotion_state = 'PROMOTED',
    promoted_reference = $4,
    promotion_readback_sha256 = $5,
    promoted_at = clock_timestamp(),
    promotion_claimant_workload = NULL,
    promotion_authority_generation = 0,
	promotion_claim_token_sha256 = NULL,
	promotion_claim_expires_at = NULL,
    promotion_authorization_token_sha256 = NULL,
    promotion_authorization_expires_at = NULL,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE organization_id = $1::uuid
  AND id = $2::uuid
  AND version = $3
  AND admission_state = 'ACCEPTED'
  AND admission_verdict = 'ACCEPTED'
  AND promotion_state = 'AUTHORIZED'
	AND promotion_authority_generation = $6
	AND promotion_fence = $7
	AND promotion_request_id = $8::uuid
	AND promotion_authorization_token_sha256 = $9
	AND promotion_authorization_expires_at > clock_timestamp()
	AND manifest_digest = $10
	AND provenance_sha256 = $11
	AND admission_receipt_sha256 = $12
	AND admission_receipt_oci_manifest_digest = $13
	AND EXISTS (
		SELECT 1
		FROM control_plane.role_image_promotion_requests request
		WHERE request.id = $8::uuid
		  AND request.organization_id = image_artifacts.organization_id
		  AND request.image_artifact_id = image_artifacts.id
		  AND request.state = 'PROMOTING'
		  AND request.receipt_sha256 = $14
		  AND request.expected_provenance_sha256 = image_artifacts.provenance_sha256
		  AND request.manifest_digest = image_artifacts.manifest_digest
	)
RETURNING version, promoted_reference, promotion_readback_sha256, promoted_at, updated_at
