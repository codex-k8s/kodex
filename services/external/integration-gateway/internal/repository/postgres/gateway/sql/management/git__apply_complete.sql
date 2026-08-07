WITH changed AS (
    UPDATE integration_gateway.git_reconciliations
       SET state = 'APPLIED', encrypted_snapshot = ''::bytea,
           target_resource_id = @target_resource_id, target_version = @target_version,
           target_sha256 = @target_sha256, updated_at = @updated_at
     WHERE reconciliation_id = @reconciliation_id AND state = 'FETCHED'
    RETURNING reconciliation_id
), completed AS (
    UPDATE integration_gateway.management_effects AS effect
       SET status = 'SUCCEEDED', lease_id = '', lease_expires_at = NULL, updated_at = @updated_at
      FROM changed
     WHERE effect.effect_id = @effect_id AND effect.status = 'CLAIMED'
       AND effect.lease_id = @lease_id AND effect.lease_fence = @lease_fence
    RETURNING effect_id
)
SELECT reconciliation_id FROM changed WHERE EXISTS (SELECT 1 FROM completed)
