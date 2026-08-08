WITH winner AS (
    SELECT receipt.test_id
      FROM integration_gateway.integration_test_receipts AS receipt
      JOIN integration_gateway.managed_provider_connections AS connection
        ON connection.connection_id = receipt.connection_id
      JOIN integration_gateway.provider_credential_generations AS credential
        ON credential.connection_id = receipt.connection_id
       AND credential.generation = receipt.credential_generation
      JOIN integration_gateway.integration_configurations AS configuration
        ON configuration.configuration_id = receipt.configuration_id
       AND configuration.version = receipt.configuration_version
      JOIN integration_gateway.management_effects AS effect ON effect.effect_id = @effect_id
     WHERE receipt.test_id = @test_id AND receipt.category = 'PENDING'
       AND connection.version = receipt.connection_version
       AND connection.generation = receipt.connection_generation
       AND connection.active_credential_generation = receipt.credential_generation
       AND connection.status = 'VALID'
       AND credential.status = 'ACTIVE'
       AND credential.credential_binding_id = receipt.credential_binding_id
       AND credential.credential_binding_version = receipt.credential_binding_version
       AND credential.credential_binding_sha256 = receipt.credential_binding_sha256
       AND configuration.configuration_sha256 = receipt.configuration_sha256
       AND configuration.definition_id = receipt.definition_id
       AND configuration.definition_version = receipt.definition_version
       AND configuration.definition_sha256 = receipt.definition_sha256
       AND configuration.connection_id = receipt.connection_id
       AND configuration.connection_version = receipt.connection_version
       AND configuration.connection_generation = receipt.connection_generation
       AND configuration.status = 'ACTIVE'
       AND effect.effect_kind = 'INTEGRATION_TEST' AND effect.resource_id = receipt.test_id
       AND effect.owner_kind = 'managed_provider_connection'
       AND effect.owner_id = receipt.connection_id
       AND effect.owner_version = receipt.connection_version
       AND effect.owner_generation = receipt.connection_generation
       AND effect.owner_status = 'VALID'
       AND effect.input_sha256 = receipt.receipt_sha256
       AND effect.status = 'CLAIMED' AND effect.lease_id = @lease_id
       AND effect.lease_fence = @lease_fence AND effect.dispatch_state = 'DISPATCHED'
       AND ((@category = 'TIMEOUT' AND receipt.expires_at <= clock_timestamp())
         OR (@category <> 'TIMEOUT' AND receipt.expires_at > clock_timestamp()))
     FOR UPDATE OF receipt, connection, credential, configuration, effect
), changed AS (
    UPDATE integration_gateway.integration_test_receipts AS receipt
       SET category = @category, receipt_sha256 = @receipt_sha256, tested_at = @tested_at
      FROM winner
     WHERE receipt.test_id = winner.test_id
    RETURNING receipt.test_id
), completed AS (
    UPDATE integration_gateway.management_effects AS effect
       SET status = 'SUCCEEDED', dispatch_state = 'COMPLETED', lease_id = '', lease_expires_at = NULL, updated_at = @tested_at
      FROM changed
     WHERE effect.effect_id = @effect_id AND effect.status = 'CLAIMED'
       AND effect.lease_id = @lease_id AND effect.lease_fence = @lease_fence
       AND effect.dispatch_state = 'DISPATCHED'
    RETURNING effect_id
)
SELECT test_id FROM changed WHERE EXISTS (SELECT 1 FROM completed)
