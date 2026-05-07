-- name: manifest_snapshot__get_latest :one
SELECT
    id,
    package_version_id,
    schema_version,
    payload,
    validation_status,
    validation_errors,
    created_at
FROM package_hub_manifest_snapshots
WHERE package_version_id = @package_version_id
ORDER BY created_at DESC, id DESC
LIMIT 1;
