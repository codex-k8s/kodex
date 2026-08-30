-- name: role_images_activate_artifact :one
UPDATE control_plane.role_image_recipes
SET active_image_artifact_id = $3::uuid,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE organization_id = $1::uuid
  AND id = $2::uuid
  AND state = 'ACTIVE'
  AND version = $4
  AND spec_sha256 = (
      SELECT artifact.spec_sha256
      FROM control_plane.image_artifacts artifact
      WHERE artifact.id = $3::uuid
  )
	AND EXISTS (
		SELECT 1
		FROM control_plane.image_artifacts artifact
		JOIN control_plane.role_image_promotion_requests request
		  ON request.id = artifact.promotion_request_id
		WHERE artifact.id = $3::uuid
		  AND artifact.organization_id = role_image_recipes.organization_id
		  AND artifact.recipe_id = role_image_recipes.id
		  AND artifact.recipe_version = role_image_recipes.version
		  AND artifact.promotion_state = 'PROMOTED'
		  AND artifact.promoted_reference <> ''
		  AND artifact.promotion_readback_sha256 ~ '^[a-f0-9]{64}$'
		  AND request.organization_id = artifact.organization_id
		  AND request.recipe_id = artifact.recipe_id
		  AND request.image_artifact_id = artifact.id
		  AND request.state = 'PROMOTING'
		  AND request.expected_provenance_sha256 = artifact.provenance_sha256
		  AND request.manifest_digest = artifact.manifest_digest
	)
RETURNING version, updated_at
