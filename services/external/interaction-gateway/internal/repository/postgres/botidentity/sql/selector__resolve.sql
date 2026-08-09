-- name: selector__resolve :one
-- params: @arg1,@arg2,@arg3,@arg4
SELECT identity.identity_ref::text, selector.selector_id::text, identity.provider_object_ref::text,
       COALESCE(identity.agent_ref::text, ''), identity.agent_stable_key,
       identity.provider_bot_id, identity.provider_user_id, identity.provider_team_id,
       identity.provider_token_id, COALESCE(identity.credential_binding_id::text, ''),
       identity.credential_secret_ref, COALESCE(identity.credential_secret_version, 0),
       identity.credential_sha256, identity.username, identity.display_name, identity.status,
       identity.provider_version, COALESCE(identity.provider_generation, 0),
       identity.provider_snapshot_sha256, identity.provider_causality_sha256,
       identity.observed_at, identity.created_at, identity.updated_at
FROM interaction_gateway_agent_bot_selectors AS selector
JOIN interaction_gateway_agent_bot_identities AS identity ON identity.identity_ref = selector.identity_ref
WHERE selector.selector_id = @arg1::uuid AND selector.organization_id = @arg2::uuid
  AND selector.project_id = @arg3::uuid AND selector.actor_id = @arg4::uuid
  AND selector.expires_at > clock_timestamp()
  AND selector.provider_snapshot_sha256 = identity.provider_snapshot_sha256
  AND NOT EXISTS (
      SELECT 1 FROM interaction_gateway_agent_bot_ownership AS ownership
      WHERE ownership.organization_id = selector.organization_id
        AND ownership.project_id = selector.project_id
        AND ownership.provider_object_ref = identity.provider_object_ref
  );
