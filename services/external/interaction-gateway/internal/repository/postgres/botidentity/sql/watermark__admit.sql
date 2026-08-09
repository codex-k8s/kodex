-- name: watermark__admit :exec
-- params: @arg1,@arg2,@arg3,@arg4,@arg5
UPDATE interaction_gateway_agent_bot_watermarks
SET admitted = @arg5::boolean, updated_at = clock_timestamp()
WHERE organization_id = @arg1::uuid AND project_id = @arg2::uuid AND agent_ref = @arg3::uuid
  AND provider_generation = @arg4::bigint;
