-- name: kubernetes_cluster__update_health :exec
UPDATE fleet_manager_kubernetes_clusters
SET last_health_status = @last_health_status,
    last_health_checked_at = @last_health_checked_at
WHERE id = @id;
