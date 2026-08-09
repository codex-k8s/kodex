-- name: binding__upsert :exec
-- params: @arg1,@arg2,@arg3,@arg4,@arg5,@arg6,@arg7,@arg8,@arg9,@arg10
INSERT INTO interaction_gateway_agent_bot_bindings(
    organization_id, project_id, agent_ref, actor_id, agent_stable_key,
    agent_version, identity_ref, provider_generation, status, receipt_sha256
) VALUES (@arg1::uuid, @arg2::uuid, @arg3::uuid, @arg4::uuid, @arg5::text,
          @arg6::bigint, @arg7::uuid, @arg8::bigint, @arg9::text, @arg10::text)
ON CONFLICT (organization_id, project_id, agent_ref) DO UPDATE SET
    actor_id = EXCLUDED.actor_id, agent_stable_key = EXCLUDED.agent_stable_key,
    agent_version = EXCLUDED.agent_version, identity_ref = EXCLUDED.identity_ref,
    provider_generation = EXCLUDED.provider_generation, status = EXCLUDED.status,
    receipt_sha256 = EXCLUDED.receipt_sha256, updated_at = clock_timestamp()
WHERE interaction_gateway_agent_bot_bindings.provider_generation < EXCLUDED.provider_generation;
