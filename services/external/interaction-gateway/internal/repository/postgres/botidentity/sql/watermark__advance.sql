-- name: watermark__advance :one
-- params: @arg1,@arg2,@arg3
INSERT INTO interaction_gateway_agent_bot_watermarks(
    organization_id, project_id, agent_ref, provider_generation, admitted
) VALUES (@arg1::uuid, @arg2::uuid, @arg3::uuid, 1, false)
ON CONFLICT (organization_id, project_id, agent_ref) DO UPDATE SET
    provider_generation = interaction_gateway_agent_bot_watermarks.provider_generation + 1,
    admitted = false, updated_at = clock_timestamp()
RETURNING provider_generation;
