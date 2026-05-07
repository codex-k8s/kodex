-- name: package_secret_schema__get_latest :one
SELECT
    id,
    package_version_id,
    schema_digest,
    fields,
    created_at
FROM package_hub_package_secret_schemas
WHERE package_version_id = @package_version_id
ORDER BY created_at DESC, id DESC
LIMIT 1;
