-- name: package_version__list :many
SELECT
    id,
    package_id,
    version_label,
    source_ref_kind,
    source_ref,
    source_commit_sha,
    manifest_digest,
    verification_status,
    release_status,
    revision,
    published_at,
    created_at,
    updated_at
FROM package_hub_package_versions
WHERE package_id = @package_id
  AND (@verification_status::text IS NULL OR verification_status = @verification_status::text)
  AND (@release_status::text IS NULL OR release_status = @release_status::text)
ORDER BY created_at DESC, id
LIMIT @limit::integer
OFFSET @offset::bigint;
