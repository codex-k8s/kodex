-- name: project__list :many
SELECT
    id, organization_id, slug, display_name, description, icon_object_uri,
    status, version, created_at, updated_at
FROM project_catalog_projects
WHERE (@organization_id::uuid IS NULL OR organization_id = @organization_id)
  AND (cardinality(@statuses::text[]) = 0 OR status = ANY(@statuses::text[]))
ORDER BY slug, id
LIMIT @limit::integer OFFSET @offset::integer;
