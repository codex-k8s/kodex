-- name: flow__list :many
SELECT
    id,
    scope_type,
    scope_ref,
    slug,
    display_name,
    description,
    icon_object_uri,
    status,
    active_version_id,
    version,
    created_at,
    updated_at
FROM agent_manager_flows
WHERE (@scope_type::text IS NULL OR scope_type = @scope_type::text)
  AND (@scope_ref::text IS NULL OR scope_ref = @scope_ref::text)
  AND (@status::text IS NULL OR status = @status::text)
ORDER BY scope_type, scope_ref, slug, id
LIMIT @limit::integer
OFFSET @offset::integer;
