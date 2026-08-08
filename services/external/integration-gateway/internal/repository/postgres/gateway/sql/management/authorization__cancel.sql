WITH closed AS (
    UPDATE integration_gateway.provider_authorization_attempts
       SET state = 'CANCELLED', version = version + 1, payload = @authorization_payload::jsonb,
           device_result_ciphertext = ''::bytea,
           lease_id = '', lease_expires_at = NULL, updated_at = @updated_at
     WHERE authorization_id = @authorization_id AND version = @expected_version
       AND state IN ('PENDING', 'CODE_ISSUED')
    RETURNING connection_id
), changed_connection AS (
    UPDATE integration_gateway.managed_provider_connections AS connection
       SET version = @connection_version, status = @connection_status,
           payload = @connection_payload::jsonb, updated_at = @updated_at
      FROM closed
     WHERE connection.connection_id = closed.connection_id AND connection.status <> 'REVOKED'
    RETURNING connection.connection_id
)
UPDATE integration_gateway.management_effects AS effect
   SET status = CASE WHEN dispatch_state = 'DISPATCHED' THEN 'UNKNOWN' ELSE 'CANCELLED' END,
       dispatch_state = 'COMPLETED', lease_id = '', lease_expires_at = NULL, updated_at = @updated_at
  FROM closed
 WHERE effect.resource_kind = 'provider_authorization'
   AND effect.resource_id = @authorization_id
   AND effect.status IN ('PENDING', 'CLAIMED')
RETURNING effect.effect_id
