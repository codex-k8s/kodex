-- name: cluster_health_snapshot__get_by_id :one
SELECT
    id, cluster_id, health_status, capacity_status, summary_json,
    checked_at, error_code, error_message
FROM fleet_manager_cluster_health_snapshots
WHERE id = @id;
