-- name: package_source__get_by_id :one
SELECT
    id,
    organization_id,
    slug,
    display_name,
    source_kind,
    repository_ref,
    catalog_endpoint_ref,
    status,
    last_sync_at,
    last_error,
    version,
    created_at,
    updated_at
FROM package_hub_package_sources
WHERE id = @id;
