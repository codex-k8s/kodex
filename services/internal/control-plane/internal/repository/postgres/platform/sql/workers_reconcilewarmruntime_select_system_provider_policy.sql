-- name: workers_reconcilewarmruntime_select_system_provider_policy :one
SELECT agent.id::text,
       agent.ref,
       runtime_config.id::text,
       runtime_config.version_number,
       runtime_config.runtime_profile_key,
       runtime_config.provider,
       runtime_config.model,
       provider_policy.mode,
       provider_policy.account_candidates,
       candidate_pool.account_candidates
FROM control_plane.assistant_runtime runtime
JOIN control_plane.agents agent
  ON agent.id = runtime.agent_id
JOIN control_plane.agent_runtime_config_versions runtime_config
  ON runtime_config.id = agent.current_runtime_config_id
JOIN control_plane.provider_account_policy_versions provider_policy
  ON provider_policy.id = runtime_config.provider_account_policy_id
JOIN LATERAL (
    SELECT COALESCE(
               jsonb_agg(
                   jsonb_build_object('accountRef', candidate.ref, 'weight', 1)
                   ORDER BY candidate.ref
               ),
               '[]'::jsonb
           ) AS account_candidates
    FROM control_plane.provider_accounts candidate
    WHERE candidate.organization_id = runtime.organization_id
      AND candidate.definition_key = runtime_config.provider
      AND candidate.current_credential_revision_id IS NOT NULL
      AND candidate.state IN ('AUTHORIZED', 'REAUTHORIZATION_REQUIRED')
) candidate_pool ON true
WHERE runtime.organization_id = @organization_id::uuid
  AND runtime.stable_key = 'system-assistant'
FOR UPDATE OF runtime, agent;
