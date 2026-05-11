-- name: kubernetes_cluster__list :many
SELECT
    id, fleet_scope_id, server_id, cluster_key, status, is_default, api_endpoint_ref,
    secret_store_type, secret_store_ref, kubernetes_version, region, capacity_class,
    last_health_status, last_health_checked_at, version, created_at, updated_at
FROM fleet_manager_kubernetes_clusters
WHERE (@fleet_scope_id::uuid IS NULL OR fleet_scope_id = @fleet_scope_id)
  AND (@server_id::uuid IS NULL OR server_id = @server_id)
  AND (cardinality(@statuses::text[]) = 0 OR status = ANY(@statuses::text[]))
  AND (cardinality(@health_statuses::text[]) = 0 OR last_health_status = ANY(@health_statuses::text[]))
  AND (@region::text = '' OR region = @region)
  AND (@capacity_class::text = '' OR capacity_class = @capacity_class)
  AND (@is_default::boolean IS NULL OR is_default = @is_default)
ORDER BY fleet_scope_id, cluster_key, id
LIMIT @limit::integer OFFSET @offset::integer;
