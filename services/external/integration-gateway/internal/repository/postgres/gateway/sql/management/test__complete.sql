WITH changed AS (
    UPDATE integration_gateway.integration_test_receipts
       SET category = @category, receipt_sha256 = @receipt_sha256, tested_at = @tested_at
     WHERE test_id = @test_id AND category = 'PENDING'
       AND ((@category = 'TIMEOUT' AND expires_at <= clock_timestamp())
         OR (@category <> 'TIMEOUT' AND expires_at > clock_timestamp()))
    RETURNING test_id
), completed AS (
    UPDATE integration_gateway.management_effects AS effect
       SET status = 'SUCCEEDED', lease_id = '', lease_expires_at = NULL, updated_at = @tested_at
      FROM changed
     WHERE effect.effect_id = @effect_id AND effect.status = 'CLAIMED'
       AND effect.lease_id = @lease_id AND effect.lease_fence = @lease_fence
    RETURNING effect_id
)
SELECT test_id FROM changed WHERE EXISTS (SELECT 1 FROM completed)
