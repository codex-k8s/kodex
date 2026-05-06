-- name: repository__get_by_id :one
SELECT
    id, project_id, provider, provider_owner, provider_name, web_url,
    default_branch, status, provider_repository_id, icon_object_uri,
    version, created_at, updated_at
FROM project_catalog_repositories
WHERE id = @id;
