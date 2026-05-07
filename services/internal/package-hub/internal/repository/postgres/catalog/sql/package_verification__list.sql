-- name: package_verification__list :many
SELECT
    id,
    package_version_id,
    verification_status,
    verified_by_actor_ref,
    verification_notes,
    created_at
FROM package_hub_package_verifications
WHERE package_version_id = @package_version_id
  AND (@verification_status::text IS NULL OR verification_status = @verification_status::text)
ORDER BY created_at DESC, id
LIMIT @limit::integer
OFFSET @offset::bigint;
