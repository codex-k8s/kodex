-- name: runtime_configuration__select_provider_account :many
SELECT account.id::text, account.ref, config.ref, config.version_number,
       config.digest, policy.ref, policy.version_number, policy.digest,
       profile.provider, COALESCE(NULLIF(candidate.value->>'model',''), config.model)
FROM control_plane.agents agent
JOIN control_plane.agent_runtime_config_versions config ON config.id=agent.current_runtime_config_id
JOIN control_plane.provider_account_policy_versions policy ON policy.id=config.provider_account_policy_id
LEFT JOIN LATERAL jsonb_array_elements(policy.account_candidates) WITH ORDINALITY candidate(value, precedence) ON policy.mode <> 'PERSONAL'
JOIN control_plane.provider_accounts account ON account.organization_id=agent.organization_id
  AND account.enabled AND account.state='AUTHORIZED' AND account.current_credential_revision_id IS NOT NULL
  AND (
    (policy.mode <> 'PERSONAL' AND account.personal_owner_subject_id IS NULL AND account.ref=candidate.value->>'accountRef')
    OR (policy.mode = 'PERSONAL' AND account.personal_owner_subject_id = NULLIF($3, '')::uuid
      AND EXISTS (SELECT 1 FROM control_plane.subjects owner
        WHERE owner.id = account.personal_owner_subject_id AND owner.organization_id = agent.organization_id
          AND owner.kind = 'USER' AND owner.active))
  )
JOIN control_plane.runtime_profiles profile
  ON profile.stable_key=COALESCE(NULLIF(candidate.value->>'runtimeProfileRef',''), config.runtime_profile_key)
 AND profile.enabled
 AND profile.provider=account.definition_key
 AND profile.provider=CASE WHEN candidate.value->>'runtimeProfileRef' IS NULL
                           THEN config.provider ELSE candidate.value->>'providerDefinitionKey' END
LEFT JOIN LATERAL (SELECT count(*)::bigint AS active_sessions FROM control_plane.sessions session
  WHERE session.provider_account_id=account.id AND session.state='ACTIVE') usage ON true
WHERE agent.organization_id=$1::uuid AND agent.ref=$2 AND agent.enabled AND agent.state='READY'
ORDER BY CASE policy.mode WHEN 'FIXED' THEN candidate.precedence::numeric
  WHEN 'LEAST_USED' THEN usage.active_sessions::numeric
  WHEN 'WEIGHTED' THEN usage.active_sessions::numeric/GREATEST((candidate.value->>'weight')::numeric,1)
  ELSE 1000000000::numeric END, account.ref
FOR UPDATE OF account;
