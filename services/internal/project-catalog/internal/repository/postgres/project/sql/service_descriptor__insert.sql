-- name: service_descriptor__insert :exec
INSERT INTO project_catalog_service_descriptors (
    id, project_id, services_policy_id, repository_id, service_key,
    display_name, kind, root_path, documentation_scope_id,
    depends_on_service_keys, status, version, created_at, updated_at
) VALUES (
    @id, @project_id, @services_policy_id, @repository_id, @service_key,
    @display_name, @kind, @root_path, @documentation_scope_id,
    @depends_on_service_keys, @status, @version, @created_at, @updated_at
);
