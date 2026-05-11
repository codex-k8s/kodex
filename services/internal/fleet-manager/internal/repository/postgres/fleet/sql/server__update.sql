-- name: server__update :exec
UPDATE fleet_manager_servers
SET
    server_key = @server_key,
    provider_type = @provider_type,
    status = @status,
    primary_address_ref = @primary_address_ref,
    region = @region,
    capacity_class = @capacity_class,
    secret_store_type = @secret_store_type,
    secret_store_ref = @secret_store_ref,
    updated_at = @updated_at,
    version = @version
WHERE id = @id AND version = @previous_version;
