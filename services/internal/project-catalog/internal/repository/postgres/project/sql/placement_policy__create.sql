-- name: placement_policy__create :exec
INSERT INTO project_catalog_placement_policies (
    id, project_id, repository_id, service_key, allowed_cluster_refs,
    status, version, created_at, updated_at
) VALUES (
    @id, @project_id, @repository_id, @service_key, @allowed_cluster_refs,
    @status, @version, @created_at, @updated_at
);
