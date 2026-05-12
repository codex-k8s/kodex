-- name: cluster_health_snapshot__list :many
SELECT
    id, cluster_id, health_status, capacity_status, summary_json,
    checked_at, error_code, error_message
FROM fleet_manager_cluster_health_snapshots
WHERE cluster_id = @cluster_id
  AND (@checked_since::timestamptz IS NULL OR checked_at >= @checked_since)
ORDER BY checked_at DESC, id DESC
LIMIT @limit::integer OFFSET @offset::integer;
