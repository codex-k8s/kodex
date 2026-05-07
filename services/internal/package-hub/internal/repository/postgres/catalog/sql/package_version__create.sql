-- name: package_version__create :exec
INSERT INTO package_hub_package_versions (
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
) VALUES (
    @id,
    @package_id,
    @version_label,
    @source_ref_kind,
    @source_ref,
    @source_commit_sha,
    @manifest_digest,
    @verification_status,
    @release_status,
    @revision,
    @published_at::timestamptz,
    @created_at,
    @updated_at
);
