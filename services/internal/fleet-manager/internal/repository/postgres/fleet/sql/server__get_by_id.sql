-- name: server__get_by_id :one
SELECT
    id, server_key, provider_type, status, primary_address_ref, region,
    capacity_class, secret_store_type, secret_store_ref, version, created_at, updated_at
FROM fleet_manager_servers
WHERE id = @id;
