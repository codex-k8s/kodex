-- name: role_images_mark_promotion_request_promoted :one
UPDATE control_plane.role_image_promotion_requests
SET state = 'PROMOTED',
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id::uuid
  AND id = @promotion_request_id::uuid
  AND image_artifact_id = @image_artifact_id::uuid
  AND expected_provenance_sha256 = @expected_provenance_sha256
  AND manifest_digest = @manifest_digest
  AND receipt_sha256 = @receipt_sha256
  AND state = 'PROMOTING'
RETURNING state;
