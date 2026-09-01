-- name: role_images_insert_promotion_request :one
INSERT INTO control_plane.role_image_promotion_requests
    (ref, organization_id, project_id, recipe_id, image_artifact_id,
     expected_provenance_sha256, manifest_digest, receipt_sha256, requested_by)
VALUES
    (@ref, @organization_id::uuid, @project_id::uuid, @recipe_id::uuid,
     @image_artifact_id::uuid, @expected_provenance_sha256, @manifest_digest,
     @receipt_sha256, @requested_by::uuid)
RETURNING id::text, state, created_at;
