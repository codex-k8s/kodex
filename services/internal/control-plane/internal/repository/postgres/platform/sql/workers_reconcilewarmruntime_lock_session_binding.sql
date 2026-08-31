-- name: workers_reconcilewarmruntime_lock_session_binding :one
SELECT agent.ref,
       session.id::text,
       session.ref,
       session.created_by::text,
       session.state = 'ACTIVE'
           AND agent.enabled
           AND agent.state = 'READY'
           AND provider_account.state = 'AUTHORIZED'
           AND provider_account.enabled
           AND provider_account.current_credential_revision_id IS NOT NULL
           AND provider_account.definition_key = runtime_config.provider
           AND EXISTS (
               SELECT 1
               FROM jsonb_array_elements(provider_policy.account_candidates) candidate(value)
               WHERE candidate.value ->> 'accountRef' = provider_account.ref
           ) AS provider_account_eligible
FROM control_plane.assistant_runtime runtime
JOIN control_plane.agents agent
  ON agent.id = runtime.agent_id
JOIN control_plane.sessions session
  ON session.ref = runtime.system_session_ref
 AND session.organization_id = runtime.organization_id
JOIN control_plane.provider_accounts provider_account
  ON provider_account.id = session.provider_account_id
 AND provider_account.organization_id = runtime.organization_id
JOIN control_plane.agent_runtime_config_versions runtime_config
  ON runtime_config.id = agent.current_runtime_config_id
JOIN control_plane.provider_account_policy_versions provider_policy
  ON provider_policy.id = runtime_config.provider_account_policy_id
WHERE runtime.organization_id = @organization_id::uuid
FOR UPDATE OF runtime, agent, session, provider_account;
