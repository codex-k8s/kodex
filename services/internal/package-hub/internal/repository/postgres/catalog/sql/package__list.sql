-- name: package__list :many
SELECT
    id,
    source_id,
    slug,
    package_kind,
    publisher_ref,
    display_name,
    description,
    icon_object_uri,
    commercial_status,
    trust_status,
    status,
    version,
    created_at,
    updated_at
FROM package_hub_packages
WHERE (@source_id::uuid IS NULL OR source_id = @source_id::uuid)
  AND (@package_kind::text IS NULL OR package_kind = @package_kind::text)
  AND (@status::text IS NULL OR status = @status::text)
  AND (@commercial_status::text IS NULL OR commercial_status = @commercial_status::text)
  AND (@trust_status::text IS NULL OR trust_status = @trust_status::text)
  AND (@query::text = '' OR slug ILIKE '%' || @query::text || '%' OR display_name::text ILIKE '%' || @query::text || '%')
ORDER BY slug, id
LIMIT @limit::integer
OFFSET @offset::bigint;
