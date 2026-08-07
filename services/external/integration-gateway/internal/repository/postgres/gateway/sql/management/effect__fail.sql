WITH selected AS (
    SELECT effect_id, effect_kind, resource_id, resource_version, resource_generation
      FROM integration_gateway.management_effects
     WHERE effect_id = @effect_id AND status = 'CLAIMED'
       AND lease_id = @lease_id AND lease_fence = @lease_fence
     FOR UPDATE
), connection_failed AS (
    UPDATE integration_gateway.managed_provider_connections AS connection
       SET version = connection.version + 1,
           status = CASE WHEN connection.active_credential_generation = 0 THEN 'INVALID' ELSE connection.status END,
           payload = jsonb_set(jsonb_set(connection.payload, '{status}',
                                         to_jsonb(CASE WHEN connection.active_credential_generation = 0 THEN 'INVALID' ELSE connection.status END)),
                               '{version}', to_jsonb(connection.version + 1)),
           updated_at = @updated_at
      FROM selected
     WHERE selected.effect_kind = 'PROVIDER_REFERENCE_SYNC'
       AND connection.connection_id = selected.resource_id
       AND connection.version = selected.resource_version
       AND connection.generation = selected.resource_generation
       AND connection.status IN ('PENDING', 'VALID')
    RETURNING connection.connection_id
), credential_failed AS (
    UPDATE integration_gateway.provider_credential_generations AS credential
       SET status = 'FAILED'
      FROM selected
     WHERE selected.effect_kind = 'PROVIDER_REFERENCE_SYNC'
       AND credential.connection_id = selected.resource_id
       AND credential.generation = selected.resource_generation
       AND credential.status = 'PENDING'
    RETURNING credential.connection_id
), pool_failed AS (
    UPDATE integration_gateway.managed_provider_pools AS pool
       SET status = CASE WHEN pool.status = 'PENDING' THEN 'ARCHIVED' ELSE pool.status END,
           payload = jsonb_set(pool.payload, '{status}',
                               to_jsonb(CASE WHEN pool.status = 'PENDING' THEN 'ARCHIVED' ELSE pool.status END)),
           updated_at = @updated_at
      FROM selected
     WHERE selected.effect_kind = 'PROVIDER_POOL_SYNC'
       AND pool.provider_pool_id = selected.resource_id
       AND pool.version = selected.resource_version
       AND pool.status IN ('PENDING', 'ARCHIVED', 'DELETED')
    RETURNING pool.provider_pool_id
), test_failed AS (
    UPDATE integration_gateway.integration_test_receipts AS test
       SET category = 'PROTOCOL_ERROR', tested_at = @updated_at
      FROM selected
     WHERE selected.effect_kind = 'INTEGRATION_TEST'
       AND test.test_id = selected.resource_id AND test.category = 'PENDING'
    RETURNING test.test_id
), git_failed AS (
    UPDATE integration_gateway.git_reconciliations AS reconciliation
       SET state = 'FAILED', failure_category = @failure_category,
           encrypted_snapshot = ''::bytea, updated_at = @updated_at
      FROM selected
     WHERE selected.effect_kind IN ('GIT_FETCH', 'GIT_APPLY')
       AND reconciliation.reconciliation_id = selected.resource_id
       AND reconciliation.state IN ('PENDING', 'FETCHED')
    RETURNING reconciliation.reconciliation_id
), completed AS (
    UPDATE integration_gateway.management_effects AS effect
       SET status = @status, lease_id = '', lease_expires_at = NULL,
           payload = jsonb_set(effect.payload, '{failure_category}', to_jsonb(@failure_category::text)),
           updated_at = @updated_at
      FROM selected
     WHERE effect.effect_id = selected.effect_id
       AND CASE selected.effect_kind
             WHEN 'PROVIDER_REFERENCE_SYNC' THEN EXISTS (SELECT 1 FROM connection_failed)
                  AND EXISTS (SELECT 1 FROM credential_failed)
             WHEN 'PROVIDER_POOL_SYNC' THEN EXISTS (SELECT 1 FROM pool_failed)
             WHEN 'INTEGRATION_TEST' THEN EXISTS (SELECT 1 FROM test_failed)
             WHEN 'GIT_FETCH' THEN EXISTS (SELECT 1 FROM git_failed)
             WHEN 'GIT_APPLY' THEN EXISTS (SELECT 1 FROM git_failed)
             ELSE true
           END
    RETURNING effect.effect_id
)
SELECT effect_id FROM completed
