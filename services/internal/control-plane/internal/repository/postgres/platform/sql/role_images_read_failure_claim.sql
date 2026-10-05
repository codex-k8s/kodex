-- name: role_images_read_failure_claim :one
SELECT admission_failure_authority_generation, admission_failure_code
FROM control_plane.image_artifacts WHERE organization_id = @organization_id::uuid AND id = @artifact_id::uuid;
