-- name: workspace_code_source__list :many
SELECT
    id, provider, provider_owner, provider_name, default_branch
FROM project_catalog_repositories
WHERE project_id = @project_id
  AND status = 'active'
  AND (cardinality(@repository_ids::uuid[]) = 0 OR id = ANY(@repository_ids::uuid[]))
ORDER BY provider, provider_owner, provider_name, id;
