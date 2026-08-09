-- name: binding__get :one
-- params: @arg1,@arg2,@arg3
SELECT binding.agent_ref::text, binding.agent_stable_key, binding.agent_version,
       binding.receipt_sha256, binding.updated_at,
       identity.identity_ref::text, ''::text, identity.provider_object_ref::text,
       COALESCE(identity.agent_ref::text, ''), identity.agent_stable_key,
       identity.provider_bot_id, identity.provider_user_id, identity.provider_team_id,
       identity.provider_token_id, COALESCE(identity.credential_binding_id::text, ''),
       identity.credential_secret_ref, COALESCE(identity.credential_secret_version, 0),
       identity.credential_sha256, identity.username, identity.display_name, identity.status,
       identity.provider_version, COALESCE(identity.provider_generation, 0),
       identity.provider_snapshot_sha256, identity.provider_causality_sha256,
       identity.observed_at, identity.created_at, identity.updated_at
FROM interaction_gateway_agent_bot_bindings AS binding
JOIN interaction_gateway_agent_bot_identities AS identity ON identity.identity_ref = binding.identity_ref
JOIN interaction_gateway_agent_bot_watermarks AS watermark
  ON watermark.organization_id = binding.organization_id AND watermark.project_id = binding.project_id
 AND watermark.agent_ref = binding.agent_ref
WHERE binding.organization_id = @arg1::uuid AND binding.project_id = @arg2::uuid
  AND binding.agent_ref = @arg3::uuid AND watermark.provider_generation = binding.provider_generation;
