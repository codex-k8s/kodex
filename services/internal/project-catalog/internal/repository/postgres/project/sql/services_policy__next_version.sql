-- name: services_policy__next_version :one
WITH locked_project AS (
    SELECT id
    FROM project_catalog_projects
    WHERE id = @project_id::uuid
    FOR UPDATE
)
SELECT COALESCE((
    SELECT max(policy_version)
    FROM project_catalog_services_policies
    WHERE project_id = locked_project.id
), 0)::bigint + 1 AS policy_version
FROM locked_project;
