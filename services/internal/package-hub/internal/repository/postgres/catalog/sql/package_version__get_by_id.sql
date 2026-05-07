-- name: package_version__get_by_id :one
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
WHERE id = @id;
