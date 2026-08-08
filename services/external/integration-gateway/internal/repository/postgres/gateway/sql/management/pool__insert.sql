INSERT INTO integration_gateway.managed_provider_pools (
    provider_pool_id, tenant_id, project_id, stable_key, display_name, policy,
    version, desired_sha256, observation_version, observation_sha256,
    effective_sha256, status, payload, created_at, updated_at
) VALUES (
    @provider_pool_id, @tenant_id, @project_id, @stable_key, @display_name, @policy,
    @version, @desired_sha256, @observation_version, @observation_sha256,
    @effective_sha256, @status, @payload::jsonb, @created_at, @updated_at
)
