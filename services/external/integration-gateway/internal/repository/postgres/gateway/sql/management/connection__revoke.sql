WITH changed AS (
    UPDATE integration_gateway.managed_provider_connections
       SET version = @version, revoke_generation = @revoke_generation,
           status = 'REVOKED', payload = @payload::jsonb, updated_at = @updated_at
     WHERE connection_id = @connection_id AND version = @expected_version
       AND generation = @expected_generation AND status <> 'REVOKED'
    RETURNING connection_id
), closed_authorizations AS (
    UPDATE integration_gateway.provider_authorization_attempts AS authorization
       SET state = 'CANCELLED', version = version + 1,
           payload = jsonb_set(authorization.payload, '{state}', '"CANCELLED"'::jsonb),
           device_result_ciphertext = ''::bytea, provider_login_id_ciphertext = ''::bytea,
           lease_id = '', lease_expires_at = NULL, updated_at = @updated_at
      FROM changed
     WHERE authorization.connection_id = changed.connection_id
       AND authorization.state IN ('PENDING', 'CODE_ISSUED')
    RETURNING authorization.authorization_id
), revoked_credentials AS (
    UPDATE integration_gateway.provider_credential_generations AS credential
       SET status = 'REVOKED', revoked_at = @updated_at
      FROM changed
     WHERE credential.connection_id = changed.connection_id AND credential.status IN ('PENDING', 'ACTIVE')
), revoked_grants AS (
    UPDATE integration_gateway.grants AS grant
       SET status = 'REVOKED', payload = jsonb_set(grant.payload, '{Status}', '"REVOKED"'::jsonb)
      FROM changed
     WHERE grant.connection_id = changed.connection_id AND grant.status = 'ACTIVE'
), cancelled_effects AS (
    UPDATE integration_gateway.management_effects AS effect
       SET status = 'CANCELLED', lease_id = '', lease_expires_at = NULL, updated_at = @updated_at
      FROM changed
     WHERE (effect.resource_id = changed.connection_id
            OR effect.resource_id IN (SELECT authorization_id FROM closed_authorizations))
       AND effect.status IN ('PENDING', 'CLAIMED')
)
SELECT connection_id FROM changed
