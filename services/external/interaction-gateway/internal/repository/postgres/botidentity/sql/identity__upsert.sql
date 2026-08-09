-- name: identity__upsert :one
-- params: @arg1,@arg2,@arg3,@arg4,@arg5,@arg6,@arg7,@arg8,@arg9,@arg10,@arg11,@arg12,@arg13,@arg14,@arg15,@arg16,@arg17,@arg18,@arg19,@arg20,@arg21,@arg22,@arg23
INSERT INTO interaction_gateway_agent_bot_identities(
    identity_ref, provider_object_ref, organization_id, project_id, agent_ref, agent_stable_key,
    provider_bot_id, provider_user_id, provider_team_id, provider_token_id,
    credential_binding_id, credential_secret_ref, credential_secret_version, credential_sha256,
    username, display_name, status, provider_version, provider_generation,
    provider_snapshot_sha256, provider_causality_sha256, observed_at, created_at, updated_at
) VALUES (@arg1::uuid, @arg2::uuid, @arg3::uuid, @arg4::uuid, NULLIF(@arg5::text, '')::uuid, @arg6::text,
          @arg7::text, @arg8::text, @arg9::text, @arg10::text, NULLIF(@arg11::text, '')::uuid,
          @arg12::text, NULLIF(@arg13::bigint, 0), @arg14::text, @arg15::text, @arg16::text,
          @arg17::text, @arg18::bigint, NULLIF(@arg19::bigint, 0), @arg20::text, @arg21::text,
          @arg22::timestamptz, @arg23::timestamptz, @arg23::timestamptz)
ON CONFLICT (identity_ref) DO UPDATE SET
    provider_object_ref = EXCLUDED.provider_object_ref,
    agent_ref = COALESCE(EXCLUDED.agent_ref, interaction_gateway_agent_bot_identities.agent_ref),
    agent_stable_key = CASE WHEN EXCLUDED.agent_stable_key = '' THEN interaction_gateway_agent_bot_identities.agent_stable_key ELSE EXCLUDED.agent_stable_key END,
    provider_bot_id = EXCLUDED.provider_bot_id, provider_team_id = EXCLUDED.provider_team_id,
    provider_token_id = CASE WHEN EXCLUDED.provider_token_id = '' THEN interaction_gateway_agent_bot_identities.provider_token_id ELSE EXCLUDED.provider_token_id END,
    credential_binding_id = COALESCE(EXCLUDED.credential_binding_id, interaction_gateway_agent_bot_identities.credential_binding_id),
    credential_secret_ref = CASE WHEN EXCLUDED.credential_secret_ref = '' THEN interaction_gateway_agent_bot_identities.credential_secret_ref ELSE EXCLUDED.credential_secret_ref END,
    credential_secret_version = COALESCE(EXCLUDED.credential_secret_version, interaction_gateway_agent_bot_identities.credential_secret_version),
    credential_sha256 = CASE WHEN EXCLUDED.credential_sha256 = '' THEN interaction_gateway_agent_bot_identities.credential_sha256 ELSE EXCLUDED.credential_sha256 END,
    username = EXCLUDED.username, display_name = EXCLUDED.display_name, status = EXCLUDED.status,
    provider_version = EXCLUDED.provider_version,
    provider_generation = COALESCE(EXCLUDED.provider_generation, interaction_gateway_agent_bot_identities.provider_generation),
    provider_snapshot_sha256 = EXCLUDED.provider_snapshot_sha256,
    provider_causality_sha256 = CASE WHEN EXCLUDED.provider_causality_sha256 = '' THEN interaction_gateway_agent_bot_identities.provider_causality_sha256 ELSE EXCLUDED.provider_causality_sha256 END,
    observed_at = EXCLUDED.observed_at, updated_at = EXCLUDED.updated_at
RETURNING identity_ref::text, ''::text, provider_object_ref::text, COALESCE(agent_ref::text, ''), agent_stable_key,
          provider_bot_id, provider_user_id, provider_team_id, provider_token_id,
          COALESCE(credential_binding_id::text, ''), credential_secret_ref,
          COALESCE(credential_secret_version, 0), credential_sha256, username, display_name, status,
          provider_version, COALESCE(provider_generation, 0), provider_snapshot_sha256,
          provider_causality_sha256, observed_at, created_at, updated_at;
