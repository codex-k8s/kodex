-- name: cluster_connectivity_check__get_by_id :one
SELECT
    id, cluster_id, status, started_at, finished_at, latency_ms,
    error_code, error_message, created_at
FROM fleet_manager_cluster_connectivity_checks
WHERE id = @id;
