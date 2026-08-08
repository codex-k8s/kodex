INSERT INTO integration_gateway.managed_provider_connections (
    connection_id, tenant_id, project_id, stable_key, provider_id, display_name,
    version, generation, revoke_generation, status, active_credential_generation,
    masked_label, masked_account, capability_sha256, observation_sha256,
    observed_at, control_plane_resource_id, control_plane_version,
    control_plane_sha256, payload, created_at, updated_at
) VALUES (
    @connection_id, @tenant_id, @project_id, @stable_key, @provider_id, @display_name,
    @version, @generation, @revoke_generation, @status, @active_credential_generation,
    @masked_label, @masked_account, @capability_sha256, @observation_sha256,
    @observed_at, @control_plane_resource_id, @control_plane_version,
    @control_plane_sha256, @payload::jsonb, @created_at, @updated_at
)
