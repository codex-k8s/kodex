-- name: cluster_health_snapshot__create :exec
INSERT INTO fleet_manager_cluster_health_snapshots (
    id, cluster_id, health_status, capacity_status, summary_json,
    checked_at, error_code, error_message
) VALUES (
    @id, @cluster_id, @health_status, @capacity_status, @summary_json,
    @checked_at, @error_code, @error_message
);
