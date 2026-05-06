-- name: project__create :exec
INSERT INTO project_catalog_projects (
    id, organization_id, slug, display_name, description, icon_object_uri,
    status, version, created_at, updated_at
) VALUES (
    @id, @organization_id, @slug, @display_name, @description, @icon_object_uri,
    @status, @version, @created_at, @updated_at
);
