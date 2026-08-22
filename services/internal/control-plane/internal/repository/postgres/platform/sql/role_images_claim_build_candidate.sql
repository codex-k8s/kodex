-- name: platform__role_images_claim_build_candidate :one
SELECT build.id::text, build.ref, build.version, build.attempt, build.maximum_attempts,
       build.fence, build.recipe_id::text, build.stage
FROM control_plane.image_builds build
WHERE build.organization_id = $1::uuid
  AND build.available_at <= clock_timestamp()
  AND (
      build.stage = 'QUEUED'
      OR (build.stage IN ('FAILED', 'EXPIRED') AND build.attempt < build.maximum_attempts)
  )
ORDER BY build.available_at, build.created_at, build.ref
FOR UPDATE SKIP LOCKED
LIMIT 1
