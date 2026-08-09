-- name: watermark__close :exec
-- params: @arg1,@arg2,@arg3,@arg4,@arg5,@arg6,@arg7
UPDATE interaction_gateway_agent_bot_watermarks
SET admitted = false, updated_at = clock_timestamp()
WHERE organization_id = @arg1::uuid AND project_id = @arg2::uuid AND agent_ref = @arg3::uuid
  AND provider_generation = @arg4::bigint
  AND EXISTS (
      SELECT 1 FROM interaction_gateway_agent_bot_operations AS operation
      WHERE operation.operation_id = @arg5::uuid AND operation.fence = @arg6::bigint
        AND operation.lease_token_sha256 = @arg7::text
        AND operation.lease_expires_at > clock_timestamp()
        AND operation.state NOT IN ('BOUND', 'REVOKED', 'REPAIR_REQUIRED')
  );
