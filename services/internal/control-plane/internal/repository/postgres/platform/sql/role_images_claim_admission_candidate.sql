-- name: role_images_claim_admission_candidate :one
SELECT artifact.id::text, artifact.ref, artifact.version, artifact.admission_fence
FROM control_plane.image_artifacts artifact
WHERE artifact.organization_id = $1::uuid
  AND (
      artifact.admission_state = 'PENDING'
      OR (artifact.admission_state = 'CLAIMED' AND artifact.admission_claim_expires_at <= clock_timestamp())
  )
ORDER BY artifact.created_at, artifact.ref
FOR UPDATE SKIP LOCKED
LIMIT 1
