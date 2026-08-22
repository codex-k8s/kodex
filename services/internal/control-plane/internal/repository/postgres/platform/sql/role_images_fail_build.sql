-- name: platform__role_images_fail_build :one
UPDATE control_plane.image_builds
SET stage = CASE WHEN attempt >= maximum_attempts THEN 'DEAD_LETTER' ELSE 'FAILED' END,
    safe_error_code = $4,
    diagnostic_code = $5,
    diagnostic_summary = $6,
    claimant_workload = NULL,
    authority_generation = 0,
    lease_token_sha256 = NULL,
    lease_expires_at = NULL,
    available_at = clock_timestamp() + interval '30 seconds',
    version = version + 1,
    updated_at = clock_timestamp()
WHERE organization_id = $1::uuid
  AND id = $2::uuid
  AND version = $3
RETURNING version, stage, safe_error_code, diagnostic_code, diagnostic_summary, updated_at
