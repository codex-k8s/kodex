-- name: package_source__list :many
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
WHERE (@organization_id::uuid IS NULL OR organization_id = @organization_id::uuid)
  AND (@source_kind::text IS NULL OR source_kind = @source_kind::text)
  AND (@status::text IS NULL OR status = @status::text)
ORDER BY slug, id
LIMIT @limit::integer
OFFSET @offset::bigint;
