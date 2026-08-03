-- name: RuntimeReconcile
WITH desired AS (
    SELECT * FROM jsonb_to_recordset(@principals::jsonb) AS candidate(
        principal_name text,
        generation bigint,
        status text,
        not_before timestamptz,
        not_after timestamptz
    )
), retired AS (
    UPDATE integration_gateway.runtime_principals SET status = 'RETIRED', updated_at = clock_timestamp()
     WHERE principal_name::text NOT IN (SELECT principal_name FROM desired)
    RETURNING principal_name
), upserted AS (
    INSERT INTO integration_gateway.runtime_principals (
        principal_name, generation, status, not_before, not_after, updated_at
    ) SELECT principal_name::name, generation, status, not_before, not_after, clock_timestamp()
      FROM desired
    ON CONFLICT (principal_name) DO UPDATE SET
        generation = EXCLUDED.generation,
        status = EXCLUDED.status,
        not_before = EXCLUDED.not_before,
        not_after = EXCLUDED.not_after,
        updated_at = clock_timestamp()
    WHERE integration_gateway.runtime_principals.status <> 'RETIRED'
      AND EXCLUDED.generation >= integration_gateway.runtime_principals.generation
      AND (
          integration_gateway.runtime_principals.status = EXCLUDED.status
          OR integration_gateway.runtime_principals.status = 'NEXT' AND EXCLUDED.status = 'CURRENT'
          OR integration_gateway.runtime_principals.status = 'CURRENT' AND EXCLUDED.status IN ('PREVIOUS', 'RETIRED')
          OR integration_gateway.runtime_principals.status = 'PREVIOUS' AND EXCLUDED.status = 'RETIRED'
      )
    RETURNING principal_name
), retired_keys AS (
    UPDATE integration_gateway.runtime_context_keys SET status = 'RETIRED', updated_at = clock_timestamp()
     WHERE key_id <> @context_key_id AND status = 'ACTIVE'
    RETURNING key_id
), reconciled_key AS (
    INSERT INTO integration_gateway.runtime_context_keys (key_id, secret, status, updated_at)
    VALUES (@context_key_id, @context_key, 'ACTIVE', clock_timestamp())
    ON CONFLICT (key_id) DO UPDATE SET
        secret = integration_gateway.runtime_context_keys.secret,
        status = integration_gateway.runtime_context_keys.status,
        updated_at = clock_timestamp()
    WHERE integration_gateway.runtime_context_keys.status = 'ACTIVE'
      AND integration_gateway.runtime_context_keys.secret = EXCLUDED.secret
    RETURNING key_id
), credential_fence AS (
    UPDATE integration_gateway.runtime_credential_fence AS fence SET
        current_high_watermark = GREATEST(fence.current_high_watermark, @current_generation),
        served_readback_generation = GREATEST(fence.served_readback_generation, @served_generation),
        updated_at = clock_timestamp()
     WHERE fence.singleton
       AND @current_generation >= fence.current_high_watermark
       AND (fence.current_high_watermark = 0
            OR @current_generation = fence.current_high_watermark
            OR @served_generation = @current_generation)
       AND NOT EXISTS (
           SELECT 1 FROM integration_gateway.runtime_principals AS previous
            WHERE previous.generation = @current_generation
              AND previous.status IN ('PREVIOUS', 'RETIRED')
       )
    RETURNING singleton
)
SELECT
    (SELECT count(*) FROM upserted) AS reconciled_principals,
    (SELECT count(*) FROM reconciled_key) AS reconciled_keys,
    (SELECT count(*) FROM credential_fence) AS reconciled_fences
