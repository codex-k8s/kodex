-- name: placement_policy__get_by_id :one
SELECT
    id, project_id, repository_id, service_key, allowed_cluster_refs,
    status, version, created_at, updated_at
FROM project_catalog_placement_policies
WHERE id = @id;
