-- name: ownership__reserve :one
-- params: @arg1,@arg2,@arg3,@arg4
INSERT INTO interaction_gateway_agent_bot_ownership (
    organization_id, project_id, provider_object_ref, agent_ref
) VALUES (@arg1::uuid, @arg2::uuid, @arg3::uuid, @arg4::uuid)
ON CONFLICT (organization_id, project_id, provider_object_ref) DO UPDATE
SET updated_at = clock_timestamp()
WHERE interaction_gateway_agent_bot_ownership.agent_ref = EXCLUDED.agent_ref
RETURNING agent_ref::text;
