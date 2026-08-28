-- name: runtime_configuration__select_provider_account :one
SELECT account.id::text,
       account.ref,
       config.ref,
       config.version_number,
       config.digest,
       policy.ref,
       policy.version_number,
       policy.digest
FROM control_plane.agents agent
JOIN control_plane.agent_runtime_config_versions config ON config.id = agent.current_runtime_config_id
JOIN control_plane.provider_account_policy_versions policy ON policy.id = config.provider_account_policy_id
JOIN LATERAL jsonb_array_elements(policy.account_candidates) candidate(value) ON true
JOIN control_plane.provider_accounts account
  ON account.organization_id = agent.organization_id
 AND account.ref = candidate.value ->> 'accountRef'
 AND account.enabled
 AND account.state = 'AUTHORIZED'
 AND account.current_credential_revision_id IS NOT NULL
JOIN control_plane.provider_definitions definition
  ON definition.stable_key = account.definition_key
 AND definition.stable_key = config.provider
LEFT JOIN LATERAL (
    SELECT count(*)::bigint AS active_sessions
    FROM control_plane.sessions session
    WHERE session.provider_account_id = account.id AND session.state = 'ACTIVE'
) usage ON true
WHERE agent.organization_id = $1::uuid
  AND agent.ref = $2
  AND agent.enabled
  AND agent.state = 'READY'
ORDER BY CASE policy.mode
             WHEN 'FIXED' THEN 0::numeric
             WHEN 'LEAST_USED' THEN usage.active_sessions::numeric
             WHEN 'WEIGHTED' THEN usage.active_sessions::numeric / GREATEST((candidate.value ->> 'weight')::numeric, 1)
             ELSE 1000000000::numeric
         END,
         account.ref
LIMIT 1
FOR UPDATE OF account;
