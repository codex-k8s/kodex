-- name: cluster_connectivity_check__create :exec
INSERT INTO fleet_manager_cluster_connectivity_checks (
    id, cluster_id, status, started_at, finished_at, latency_ms,
    error_code, error_message, created_at
) VALUES (
    @id, @cluster_id, @status, @started_at, @finished_at, @latency_ms,
    @error_code, @error_message, @created_at
);
