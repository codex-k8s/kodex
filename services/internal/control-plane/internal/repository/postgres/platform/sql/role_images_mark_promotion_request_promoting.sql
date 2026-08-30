-- name: role_images_mark_promotion_request_promoting :one
UPDATE control_plane.role_image_promotion_requests
SET state = 'PROMOTING',
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id::uuid
	AND id = @promotion_request_id::uuid
  AND image_artifact_id = @image_artifact_id::uuid
	AND receipt_sha256 = @receipt_sha256
  AND expected_provenance_sha256 = @expected_provenance_sha256
  AND manifest_digest = @manifest_digest
  AND state IN ('QUEUED', 'PROMOTING')
RETURNING id::text;
