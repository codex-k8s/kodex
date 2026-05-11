-- +goose Up
ALTER TABLE fleet_manager_kubernetes_clusters
    DROP CONSTRAINT IF EXISTS fleet_manager_kubernetes_clusters_cluster_key_key;

CREATE UNIQUE INDEX fleet_manager_kubernetes_clusters_scope_key_uidx
    ON fleet_manager_kubernetes_clusters (fleet_scope_id, cluster_key);

-- +goose Down
DROP INDEX IF EXISTS fleet_manager_kubernetes_clusters_scope_key_uidx;

ALTER TABLE fleet_manager_kubernetes_clusters
    ADD CONSTRAINT fleet_manager_kubernetes_clusters_cluster_key_key UNIQUE (cluster_key);
