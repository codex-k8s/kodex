-- name: role_images_claim_promotion_candidate :one
SELECT artifact.id::text, artifact.ref, artifact.version, artifact.promotion_fence
FROM control_plane.image_artifacts artifact
WHERE artifact.organization_id = $1::uuid
  AND artifact.admission_state = 'ACCEPTED'
  AND (
      artifact.promotion_state = 'PENDING'
      OR (artifact.promotion_state = 'CLAIMED' AND artifact.promotion_claim_expires_at <= clock_timestamp())
  )
ORDER BY artifact.created_at, artifact.ref
FOR UPDATE SKIP LOCKED
LIMIT 1
