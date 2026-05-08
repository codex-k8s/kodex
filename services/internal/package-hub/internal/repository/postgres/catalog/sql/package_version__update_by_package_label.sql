-- name: package_version__update_by_package_label :one
WITH existing AS (
    SELECT
        id,
        (
            source_ref_kind IS DISTINCT FROM @source_ref_kind
            OR source_ref IS DISTINCT FROM @source_ref
            OR source_commit_sha IS DISTINCT FROM @source_commit_sha
            OR manifest_digest IS DISTINCT FROM @manifest_digest
        ) AS content_changed,
        (
            release_status IS DISTINCT FROM @release_status
            OR published_at IS DISTINCT FROM @published_at::timestamptz
        ) AS metadata_changed
    FROM package_hub_package_versions
    WHERE package_id = @package_id
      AND version_label = @version_label
    FOR UPDATE
),
updated AS (
    UPDATE package_hub_package_versions v
    SET
        source_ref_kind = @source_ref_kind,
        source_ref = @source_ref,
        source_commit_sha = @source_commit_sha,
        manifest_digest = @manifest_digest,
        verification_status = CASE WHEN existing.content_changed THEN @verification_status ELSE v.verification_status END,
        release_status = @release_status,
        revision = CASE WHEN existing.content_changed OR existing.metadata_changed THEN v.revision + 1 ELSE v.revision END,
        published_at = @published_at::timestamptz,
        updated_at = CASE WHEN existing.content_changed OR existing.metadata_changed THEN @updated_at ELSE v.updated_at END
    FROM existing
    WHERE v.id = existing.id
    RETURNING
        v.id,
        v.package_id,
        v.version_label,
        v.source_ref_kind,
        v.source_ref,
        v.source_commit_sha,
        v.manifest_digest,
        v.verification_status,
        v.release_status,
        v.revision,
        v.published_at,
        v.created_at,
        v.updated_at,
        (existing.content_changed OR existing.metadata_changed) AS changed
)
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
    updated_at,
    false AS inserted,
    changed
FROM updated;
