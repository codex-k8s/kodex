-- name: repository__create :exec
INSERT INTO project_catalog_repositories (
    id, project_id, provider, provider_owner, provider_name, web_url,
    default_branch, status, provider_repository_id, icon_object_uri,
    version, created_at, updated_at
) VALUES (
    @id, @project_id, @provider, @provider_owner, @provider_name, @web_url,
    @default_branch, @status, @provider_repository_id, @icon_object_uri,
    @version, @created_at, @updated_at
);
