-- name: role_images_activate_artifact :exec
UPDATE control_plane.role_image_recipes
SET active_image_artifact_id = $3::uuid,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE organization_id = $1::uuid
  AND id = $2::uuid
  AND state = 'ACTIVE'
  AND spec_sha256 = (
      SELECT artifact.spec_sha256
      FROM control_plane.image_artifacts artifact
      WHERE artifact.id = $3::uuid
  )
