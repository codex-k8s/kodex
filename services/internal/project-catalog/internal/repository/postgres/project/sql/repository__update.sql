-- name: repository__update :exec
UPDATE project_catalog_repositories
SET
    default_branch = @default_branch,
    status = @status,
    icon_object_uri = @icon_object_uri,
    version = @version,
    updated_at = @updated_at
WHERE id = @id AND version = @previous_version;
