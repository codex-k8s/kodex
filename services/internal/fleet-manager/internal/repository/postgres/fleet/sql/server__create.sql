-- name: server__create :exec
INSERT INTO fleet_manager_servers (
    id, server_key, provider_type, status, primary_address_ref, region,
    capacity_class, secret_store_type, secret_store_ref, created_at, updated_at, version
) VALUES (
    @id, @server_key, @provider_type, @status, @primary_address_ref, @region,
    @capacity_class, @secret_store_type, @secret_store_ref, @created_at, @updated_at, @version
);
