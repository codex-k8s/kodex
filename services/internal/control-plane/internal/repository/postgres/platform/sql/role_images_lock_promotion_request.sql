-- name: role_images_lock_promotion_request :one
SELECT request.id::text, request.ref, request.expected_provenance_sha256,
       request.manifest_digest, request.receipt_sha256, request.state,
       request.requested_by::text, request.created_at
FROM control_plane.role_image_promotion_requests request
WHERE request.organization_id = @organization_id::uuid
  AND request.image_artifact_id = @image_artifact_id::uuid
FOR UPDATE;
