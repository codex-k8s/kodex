-- name: role_images_find_promoted_artifact :one
SELECT artifact.id::text, artifact.ref
FROM control_plane.image_artifacts artifact
WHERE artifact.organization_id = $1::uuid
  AND artifact.recipe_id = $2::uuid
  AND artifact.spec_sha256 = $3
  AND artifact.admission_state = 'ACCEPTED'
  AND artifact.promotion_state = 'PROMOTED'
  AND artifact.promoted_reference <> ''
ORDER BY artifact.promoted_at DESC
LIMIT 1
