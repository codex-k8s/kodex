-- name: cluster_health_snapshot__get_latest :one
SELECT
    id, cluster_id, health_status, capacity_status, summary_json,
    checked_at, error_code, error_message
FROM fleet_manager_cluster_health_snapshots
WHERE cluster_id = @cluster_id
ORDER BY checked_at DESC, id DESC
LIMIT 1;
