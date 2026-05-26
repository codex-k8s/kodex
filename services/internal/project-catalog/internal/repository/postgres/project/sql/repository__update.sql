-- name: repository__update :exec
UPDATE project_catalog_repositories
SET
    provider_owner = @provider_owner,
    provider_name = @provider_name,
    web_url = @web_url,
    default_branch = @default_branch,
    status = @status,
    provider_repository_id = @provider_repository_id,
    icon_object_uri = @icon_object_uri,
    version = @version,
    updated_at = @updated_at
WHERE id = @id AND version = @previous_version;
