-- name: role_images_progress_build :one
UPDATE control_plane.image_builds
SET stage = $4,
    progress_percent = $5,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE organization_id = $1::uuid
  AND id = $2::uuid
  AND version = $3
RETURNING version, stage, progress_percent, updated_at
