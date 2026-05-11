-- name: server__list :many
SELECT
    id, server_key, provider_type, status, primary_address_ref, region,
    capacity_class, secret_store_type, secret_store_ref, version, created_at, updated_at
FROM fleet_manager_servers
WHERE (cardinality(@statuses::text[]) = 0 OR status = ANY(@statuses::text[]))
  AND (cardinality(@provider_types::text[]) = 0 OR provider_type = ANY(@provider_types::text[]))
  AND (@region::text = '' OR region = @region)
  AND (@capacity_class::text = '' OR capacity_class = @capacity_class)
ORDER BY server_key, id
LIMIT @limit::integer OFFSET @offset::integer;
