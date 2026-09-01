-- name: role_images_claim_promotion :one
UPDATE control_plane.image_artifacts AS artifact
SET promotion_state = 'CLAIMED',
    promotion_claimant_workload = $4,
    promotion_authority_generation = $5,
    promotion_fence = $6,
    promotion_claim_token_sha256 = $7,
    promotion_claim_expires_at = $8,
    promotion_authorization_token_sha256 = NULL,
    promotion_authorization_expires_at = NULL,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE artifact.organization_id = $1::uuid
  AND artifact.id = $2::uuid
  AND artifact.version = $3
  AND artifact.admission_state = 'ACCEPTED'
  AND artifact.admission_verdict = 'ACCEPTED'
  AND artifact.promotion_request_id = $9::uuid
  AND EXISTS (
	  SELECT 1
	  FROM control_plane.role_image_promotion_requests request
	  WHERE request.id = $9::uuid
	    AND request.organization_id = artifact.organization_id
	    AND request.image_artifact_id = artifact.id
	    AND request.receipt_sha256 = $10
	    AND request.expected_provenance_sha256 = artifact.provenance_sha256
	    AND request.manifest_digest = artifact.manifest_digest
	    AND (
	        (request.state = 'QUEUED' AND artifact.promotion_state = 'PENDING')
	        OR (request.state = 'PROMOTING' AND artifact.promotion_state = 'CLAIMED'
	            AND artifact.promotion_claim_expires_at <= clock_timestamp())
	        OR (request.state = 'PROMOTING' AND artifact.promotion_state = 'AUTHORIZED'
	            AND artifact.promotion_authorization_expires_at <= clock_timestamp())
	    )
  )
RETURNING artifact.version, artifact.updated_at
