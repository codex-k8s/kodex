-- name: project__update :exec
UPDATE project_catalog_projects
SET
    slug = @slug,
    display_name = @display_name,
    description = @description,
    icon_object_uri = @icon_object_uri,
    status = @status,
    version = @version,
    updated_at = @updated_at
WHERE id = @id AND version = @previous_version;
