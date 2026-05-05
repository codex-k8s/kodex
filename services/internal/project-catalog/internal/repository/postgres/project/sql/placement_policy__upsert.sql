-- name: placement_policy__upsert :exec
INSERT INTO project_catalog_placement_policies (
    id, project_id, repository_id, service_key, allowed_cluster_refs,
    status, version, created_at, updated_at
) VALUES (
    @id, @project_id, @repository_id, @service_key, @allowed_cluster_refs,
    @status, @version, @created_at, @updated_at
)
ON CONFLICT (id) DO UPDATE SET
    repository_id = EXCLUDED.repository_id,
    service_key = EXCLUDED.service_key,
    allowed_cluster_refs = EXCLUDED.allowed_cluster_refs,
    status = EXCLUDED.status,
    version = EXCLUDED.version,
    updated_at = EXCLUDED.updated_at
WHERE project_catalog_placement_policies.project_id = EXCLUDED.project_id;
