-- name: repository__list :many
SELECT
    id, project_id, provider, provider_owner, provider_name, web_url,
    default_branch, status, provider_repository_id, icon_object_uri,
    version, created_at, updated_at
FROM project_catalog_repositories
WHERE project_id = @project_id
  AND (cardinality(@statuses::text[]) = 0 OR status = ANY(@statuses::text[]))
ORDER BY provider, provider_owner, provider_name, id
LIMIT @limit OFFSET @offset;
