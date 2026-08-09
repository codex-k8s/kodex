-- name: operation__finish :exec
-- params: @arg1,@arg2,@arg3,@arg4,@arg5,@arg6,@arg7,@arg8,@arg9
UPDATE interaction_gateway_agent_bot_operations
SET state = @arg2::text, receipt_id = @arg3::uuid, receipt_revision = @arg4::bigint,
    receipt_sha256 = @arg5::text, command_intent_sha256 = @arg6::text,
    result_agent_version = @arg7::bigint, lease_owner = '',
    lease_token_sha256 = repeat('0', 64), lease_expires_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE operation_id = @arg1::uuid AND state = 'PROVIDER_ACCEPTED'
  AND fence = @arg8::bigint AND lease_token_sha256 = @arg9::text
  AND lease_expires_at > clock_timestamp();
