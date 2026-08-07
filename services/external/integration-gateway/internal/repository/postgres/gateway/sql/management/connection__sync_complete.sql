WITH credential_activated AS (
    UPDATE integration_gateway.provider_credential_generations
       SET status = 'ACTIVE', activated_at = @observed_at
     WHERE connection_id = @connection_id AND generation = @expected_generation
       AND status = 'PENDING'
    RETURNING connection_id, generation
), changed AS (
    UPDATE integration_gateway.managed_provider_connections AS connection
       SET version = version + 1, status = 'VALID', observation_sha256 = @observation_sha256,
           observed_at = @observed_at, control_plane_resource_id = @control_plane_resource_id,
           control_plane_version = @control_plane_version, control_plane_sha256 = @control_plane_sha256,
           active_credential_generation = @active_credential_generation,
           masked_account = @masked_account, masked_label = @masked_label,
           payload = @payload::jsonb, updated_at = @observed_at
      FROM credential_activated
     WHERE connection.connection_id = credential_activated.connection_id
       AND connection.connection_id = @connection_id AND connection.version = @expected_version
       AND connection.generation = @expected_generation AND connection.status IN ('PENDING', 'VALID')
    RETURNING connection.connection_id
), completed AS (
    UPDATE integration_gateway.management_effects AS effect
       SET status = 'SUCCEEDED', lease_id = '', lease_expires_at = NULL, updated_at = @observed_at
      FROM changed
     WHERE effect.effect_id = @effect_id AND effect.status = 'CLAIMED'
       AND effect.lease_id = @lease_id AND effect.lease_fence = @lease_fence
    RETURNING effect.effect_id
)
SELECT connection_id FROM changed WHERE EXISTS (SELECT 1 FROM completed)
