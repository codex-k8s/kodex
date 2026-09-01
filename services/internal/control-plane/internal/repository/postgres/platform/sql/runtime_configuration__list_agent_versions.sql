-- name: runtime_configuration__list_agent_versions :many
SELECT config.ref,
       config.version_number,
       agent.ref,
       config.runtime_profile_key,
       config.provider,
       config.model,
       config.digest,
       config.created_at,
       policy.ref,
       policy.version_number,
       policy.mode,
       policy.account_candidates,
       policy.digest,
       policy.created_at
FROM control_plane.agents agent
JOIN control_plane.agent_runtime_config_versions config ON config.agent_id = agent.id
JOIN control_plane.provider_account_policy_versions policy ON policy.id = config.provider_account_policy_id
WHERE agent.organization_id = @organization_id::uuid
  AND agent.ref = @agent_ref
  AND (@before_version::bigint = 0 OR config.version_number < @before_version::bigint)
  AND (agent.system_key = 'system-assistant' OR @platform_role IN ('OWNER', 'ADMINISTRATOR') OR EXISTS (
      SELECT 1 FROM control_plane.memberships membership
      WHERE membership.project_id = agent.project_id
        AND membership.subject_id = @actor_id::uuid
        AND membership.active
        AND 'VIEW' = ANY(membership.permissions)
  ))
ORDER BY config.version_number DESC
LIMIT @page_size;
