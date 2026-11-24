-- name: runtime_catalog__read_session_binding :one
SELECT binding.catalog_revision, binding.catalog_digest, binding.models,
       account.definition_key, policy.id::text, policy.ref, policy.version_number, policy.digest,
       COALESCE(route.value->>'runtimeProfileRef', original.runtime_profile_key, ''),
       COALESCE(route.value->>'model', original.model, '')
FROM control_plane.session_model_catalog_bindings binding
JOIN control_plane.sessions session ON session.id = binding.session_id AND session.organization_id = binding.organization_id
  AND session.provider_account_id = binding.provider_account_id
JOIN control_plane.provider_accounts account ON account.id = binding.provider_account_id AND account.organization_id = binding.organization_id
JOIN control_plane.provider_account_policy_versions policy ON policy.id = binding.provider_account_policy_id AND policy.organization_id = binding.organization_id
LEFT JOIN control_plane.agent_runtime_config_versions original ON original.agent_id=policy.agent_id
  AND original.version_number=policy.version_number AND original.provider_account_policy_id=policy.id
  AND policy.mode='FIXED' AND (policy.account_candidates->1) ? 'runtimeProfileRef'
LEFT JOIN LATERAL (
  SELECT candidate AS value FROM jsonb_array_elements(policy.account_candidates) candidate
  WHERE candidate->>'accountRef'=account.ref
) route ON original.id IS NOT NULL
WHERE binding.session_id = $1::uuid AND binding.organization_id = $2::uuid AND account.ref = $3;
