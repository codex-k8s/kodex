-- name: kubernetes_cluster__update :exec
UPDATE fleet_manager_kubernetes_clusters
SET
    fleet_scope_id = @fleet_scope_id,
    server_id = @server_id,
    cluster_key = @cluster_key,
    status = @status,
    is_default = @is_default,
    api_endpoint_ref = @api_endpoint_ref,
    secret_store_type = @secret_store_type,
    secret_store_ref = @secret_store_ref,
    kubernetes_version = @kubernetes_version,
    region = @region,
    capacity_class = @capacity_class,
    last_health_status = @last_health_status,
    last_health_checked_at = @last_health_checked_at,
    updated_at = @updated_at,
    version = @version
WHERE id = @id AND version = @previous_version;
