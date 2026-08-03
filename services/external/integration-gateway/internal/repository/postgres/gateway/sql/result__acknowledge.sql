-- name: ResultAcknowledge
WITH selected AS (
    SELECT result.invocation_id, result.payload, result.delivery_version,
           result.delivery_fence, result.acknowledged_at, result.completed_at
      FROM integration_gateway.results AS result
     WHERE result.invocation_id = @invocation_id
       AND result.attempt_id = @attempt_id
       AND result.tenant_id = @tenant_id
       AND result.project_id = @project_id
       AND result.status = 'SUCCEEDED'
       AND result.delivery_version = @delivery_version
       AND result.delivery_fence = @delivery_fence
     FOR UPDATE
), receipt AS (
    INSERT INTO integration_gateway.result_delivery_receipts (
        tenant_id, project_id, invocation_id, idempotency_hash, request_hash,
        delivery_version, delivery_fence, acknowledged_at
    )
    SELECT @tenant_id, @project_id, selected.invocation_id, @idempotency_hash,
           @request_hash, selected.delivery_version, selected.delivery_fence,
           @acknowledged_at
      FROM selected
    ON CONFLICT (tenant_id, project_id, idempotency_hash) DO UPDATE
        SET idempotency_hash = EXCLUDED.idempotency_hash
      WHERE integration_gateway.result_delivery_receipts.request_hash = EXCLUDED.request_hash
        AND integration_gateway.result_delivery_receipts.invocation_id = EXCLUDED.invocation_id
        AND integration_gateway.result_delivery_receipts.delivery_version = EXCLUDED.delivery_version
        AND integration_gateway.result_delivery_receipts.delivery_fence = EXCLUDED.delivery_fence
    RETURNING acknowledged_at
), updated AS (
    UPDATE integration_gateway.results AS result
       SET acknowledged_at = COALESCE(result.acknowledged_at, receipt.acknowledged_at)
      FROM selected, receipt
     WHERE result.invocation_id = selected.invocation_id
    RETURNING result.payload, result.delivery_version, result.delivery_fence,
              result.acknowledged_at, result.completed_at
)
SELECT payload, delivery_version, delivery_fence, acknowledged_at, completed_at
  FROM updated
