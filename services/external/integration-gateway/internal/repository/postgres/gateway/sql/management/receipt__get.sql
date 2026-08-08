SELECT resource_kind, resource_id, result_version, result_sha256, request_sha256, result_payload
  FROM integration_gateway.management_idempotency_receipts
 WHERE tenant_id = @tenant_id AND project_id = @project_id
   AND operation = @operation AND key_sha256 = @key_sha256
