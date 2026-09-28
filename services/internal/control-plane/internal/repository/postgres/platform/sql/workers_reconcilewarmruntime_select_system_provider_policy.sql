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
    WITH eligible AS (
        SELECT candidate.ref,
               COALESCE(auth_attempt.method, '') AS authorization_method
        FROM control_plane.provider_accounts candidate
        LEFT JOIN LATERAL (
            SELECT attempt.method
            FROM control_plane.provider_authorization_attempts attempt
            WHERE attempt.organization_id = candidate.organization_id
              AND attempt.provider_account_id = candidate.id
              AND attempt.state = 'AUTHORIZED'
              AND attempt.preparation_state = 'APPLIED'
            ORDER BY attempt.updated_at DESC, attempt.id DESC
            LIMIT 1
        ) auth_attempt ON true
        WHERE candidate.organization_id = runtime.organization_id
          AND candidate.definition_key = runtime_config.provider
          AND candidate.current_credential_revision_id IS NOT NULL
          AND candidate.state IN ('AUTHORIZED', 'REAUTHORIZATION_REQUIRED')
    )
    SELECT COALESCE(
               jsonb_agg(
                   jsonb_build_object('accountRef', candidate.ref, 'weight', 1)
                   ORDER BY candidate.ref
               ),
               '[]'::jsonb
           ) AS account_candidates
    FROM eligible candidate
    WHERE candidate.authorization_method = 'DEVICE_CODE'
       OR NOT EXISTS (
           SELECT 1 FROM eligible preferred
           WHERE preferred.authorization_method = 'DEVICE_CODE'
       )
) candidate_pool ON true
WHERE runtime.organization_id = @organization_id::uuid
  AND runtime.stable_key = 'system-assistant'
FOR UPDATE OF runtime, agent;
