-- name: kubernetes_cluster__get_by_id :one
SELECT
    id, fleet_scope_id, server_id, cluster_key, status, is_default, api_endpoint_ref,
    secret_store_type, secret_store_ref, kubernetes_version, region, capacity_class,
    last_health_status, last_health_checked_at, version, created_at, updated_at
FROM fleet_manager_kubernetes_clusters
WHERE id = @id;
