-- name: repository__get_by_provider_ref :one
SELECT
    id, project_id, provider, provider_owner, provider_name, web_url,
    default_branch, status, provider_repository_id, icon_object_uri,
    version, created_at, updated_at
FROM project_catalog_repositories
WHERE provider = @provider
  AND provider_owner = @provider_owner
  AND provider_name = @provider_name
  AND status <> 'archived';
