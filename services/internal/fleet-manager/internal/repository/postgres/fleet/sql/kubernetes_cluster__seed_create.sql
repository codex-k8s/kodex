-- name: kubernetes_cluster__seed_create :execrows
INSERT INTO fleet_manager_kubernetes_clusters (
    id, fleet_scope_id, server_id, cluster_key, status, is_default, api_endpoint_ref,
    secret_store_type, secret_store_ref, kubernetes_version, region, capacity_class,
    last_health_status, last_health_checked_at, created_at, updated_at, version
) VALUES (
    @id, @fleet_scope_id, @server_id, @cluster_key, @status, @is_default, @api_endpoint_ref,
    @secret_store_type, @secret_store_ref, @kubernetes_version, @region, @capacity_class,
    @last_health_status, @last_health_checked_at, @created_at, @updated_at, @version
)
ON CONFLICT (id) DO NOTHING;
