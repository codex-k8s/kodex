WITH previous AS (
    SELECT authorization.connection_id, authorization.state,
           authorization.state IN ('PENDING', 'CODE_ISSUED') AS was_open
      FROM integration_gateway.provider_authorization_attempts AS authorization
     WHERE authorization.authorization_id = @previous_id
       AND authorization.version = @previous_version
       AND authorization.state IN ('PENDING', 'CODE_ISSUED', 'AUTHORIZED', 'DENIED', 'EXPIRED', 'FAILED')
     FOR UPDATE
), open_closed AS (
    UPDATE integration_gateway.provider_authorization_attempts AS authorization
       SET state = 'CANCELLED', version = authorization.version + 1,
           payload = @previous_payload::jsonb,
           device_result_ciphertext = ''::bytea, provider_login_id_ciphertext = ''::bytea,
           lease_id = '', lease_expires_at = NULL, updated_at = @updated_at
      FROM previous
     WHERE authorization.authorization_id = @previous_id AND previous.was_open
    RETURNING authorization.connection_id
), connection_changed AS (
    UPDATE integration_gateway.managed_provider_connections AS connection
       SET version = @connection_version, generation = @connection_generation,
           status = @connection_status, payload = @connection_payload::jsonb, updated_at = @updated_at
      FROM previous
     WHERE connection.connection_id = previous.connection_id AND connection.status <> 'REVOKED'
    RETURNING connection.connection_id, previous.was_open
), old_effect_cancelled AS (
    UPDATE integration_gateway.management_effects AS effect
       SET status = 'CANCELLED', lease_id = '', lease_expires_at = NULL, updated_at = @updated_at
      FROM open_closed
     WHERE effect.resource_kind = 'provider_authorization'
       AND effect.resource_id = @previous_id
       AND effect.status IN ('PENDING', 'CLAIMED')
    RETURNING effect.effect_id
)
INSERT INTO integration_gateway.provider_authorization_attempts (
    authorization_id, tenant_id, project_id, connection_id, provider_id,
    attempt, version, generation, state, intent_sha256, expires_at,
    failure_category, payload, created_at, updated_at
)
SELECT @authorization_id, @tenant_id, @project_id, connection_id, @provider_id,
       @attempt, @version, @generation, @state, @intent_sha256, @expires_at,
       @failure_category, @payload::jsonb, @created_at, @updated_at
  FROM connection_changed
 WHERE NOT was_open OR EXISTS (SELECT 1 FROM old_effect_cancelled)
RETURNING authorization_id
