-- name: ReceiptGet
SELECT receipt.request_hash, invocation.payload
  FROM integration_gateway.idempotency_receipts AS receipt
  JOIN integration_gateway.invocations AS invocation ON invocation.invocation_id = receipt.invocation_id
 WHERE receipt.tenant_id = @tenant_id AND receipt.project_id = @project_id
   AND receipt.key_hash = @key_hash
 FOR UPDATE OF receipt, invocation
