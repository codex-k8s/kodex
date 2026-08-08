WITH locked AS (
    SELECT authorization.connection_id, authorization.generation, authorization.version
      FROM integration_gateway.provider_authorization_attempts AS authorization
      JOIN integration_gateway.managed_provider_connections AS connection
        ON connection.connection_id = authorization.connection_id
     WHERE authorization.authorization_id = @authorization_id
       AND authorization.state = 'CODE_ISSUED'
       AND authorization.lease_id = @lease_id AND authorization.lease_generation = @lease_generation
       AND authorization.expires_at > clock_timestamp()
       AND authorization.code_expires_at > clock_timestamp()
       AND connection.status <> 'REVOKED' AND connection.generation = authorization.generation
     FOR UPDATE OF authorization, connection
), credential AS (
    INSERT INTO integration_gateway.provider_credential_generations (
        connection_id, generation, tenant_id, project_id, authorization_id,
        status, secret_ref, secret_version, secret_content_sha256,
        credential_binding_id, credential_binding_version, credential_binding_sha256,
        masked_account, masked_label, observed_usage, observed_limit, observation_revision,
        observed_at, window_duration_seconds, resets_at, observation_expires_at, observation_sha256, created_at
    ) SELECT connection_id, generation, @tenant_id, @project_id, @authorization_id,
             'PENDING', @secret_ref, @secret_version, @secret_content_sha256,
             @credential_binding_id, @credential_binding_version, @credential_binding_sha256,
             @masked_account, @masked_label, @observed_usage, @observed_limit, @observation_revision,
             @capacity_observed_at, @window_duration_seconds, @resets_at, @observation_expires_at, @observation_sha256, @updated_at
        FROM locked
    ON CONFLICT (connection_id, generation) DO NOTHING
    RETURNING connection_id, generation
), completed AS (
    UPDATE integration_gateway.provider_authorization_attempts AS authorization
       SET state = 'AUTHORIZED', version = authorization.version + 1,
           device_result_ciphertext = ''::bytea,
           lease_id = '', lease_expires_at = NULL, updated_at = @updated_at,
           payload = jsonb_set(jsonb_set(authorization.payload, '{state}', '"AUTHORIZED"'::jsonb),
                               '{version}', to_jsonb(authorization.version + 1))
      FROM credential
     WHERE authorization.authorization_id = @authorization_id
    RETURNING authorization.connection_id, authorization.generation
), connection_changed AS (
    UPDATE integration_gateway.managed_provider_connections AS connection
       SET payload = jsonb_set(connection.payload, '{updated_at}', to_jsonb(@updated_at::timestamptz)),
           updated_at = @updated_at
      FROM completed
     WHERE connection.connection_id = completed.connection_id
    RETURNING connection.connection_id, connection.version, connection.generation, connection.status
), authorization_effect_completed AS (
    UPDATE integration_gateway.management_effects AS effect
       SET status = 'SUCCEEDED', dispatch_state = 'COMPLETED', lease_id = '', lease_expires_at = NULL, updated_at = @updated_at
      FROM connection_changed
     WHERE effect.effect_id = @authorization_effect_id AND effect.status = 'CLAIMED'
       AND effect.lease_id = @lease_id AND effect.lease_fence = @lease_generation
       AND effect.dispatch_state = 'DISPATCHED'
    RETURNING effect.effect_id
)
INSERT INTO integration_gateway.management_effects (
    effect_id, tenant_id, project_id, actor_id, effect_kind, resource_kind, resource_id,
    resource_version, resource_generation, intent_sha256, owner_kind, owner_id,
    owner_version, owner_generation, owner_status, input_sha256, status, available_at,
    payload, created_at, updated_at
)
SELECT @effect_id, @tenant_id, @project_id, @actor_id, 'PROVIDER_REFERENCE_SYNC',
       'managed_provider_connection', connection_id, version, generation,
       @intent_sha256, 'managed_provider_connection', connection_id, version, generation,
       status, @intent_sha256, 'PENDING', @updated_at, @effect_payload::jsonb, @updated_at, @updated_at
  FROM connection_changed WHERE EXISTS (SELECT 1 FROM authorization_effect_completed)
RETURNING effect_id
