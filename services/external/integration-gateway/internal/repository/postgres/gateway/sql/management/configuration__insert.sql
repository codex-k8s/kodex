INSERT INTO integration_gateway.integration_configurations (
    configuration_id, tenant_id, project_id, stable_key, version, configuration_sha256,
    definition_id, definition_version, definition_sha256, connection_id,
    connection_version, connection_generation, capability_sha256, effect_kind,
    status, payload, created_at, updated_at
) VALUES (
    @configuration_id, @tenant_id, @project_id, @stable_key, @version, @configuration_sha256,
    @definition_id, @definition_version, @definition_sha256, @connection_id,
    @connection_version, @connection_generation, @capability_sha256, @effect_kind,
    @status, @payload::jsonb, @created_at, @updated_at
)
