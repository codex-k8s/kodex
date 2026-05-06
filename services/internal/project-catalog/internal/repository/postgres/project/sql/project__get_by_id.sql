-- name: project__get_by_id :one
SELECT
    id, organization_id, slug, display_name, description, icon_object_uri,
    status, version, created_at, updated_at
FROM project_catalog_projects
WHERE id = @id;
