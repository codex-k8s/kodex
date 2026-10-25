-- name: runtime_claimexecution_lock_environment_candidate :one
WITH pinned_environment AS MATERIALIZED (
    SELECT environment.id, environment.current_version_id, environment.project_id, environment.scope_kind
    FROM control_plane.runtime_environment_sets environment
    WHERE environment.organization_id=@organization_id::uuid
      AND environment.ref=@environment_ref
      AND environment.state='ACTIVE'
    FOR SHARE OF environment SKIP LOCKED
), pinned_agent AS MATERIALIZED (
    SELECT agent.id, agent.project_id, agent.system_key, environment.id AS environment_id,
           environment.current_version_id
    FROM pinned_environment environment
    JOIN control_plane.agents agent ON agent.organization_id=@organization_id::uuid
      AND agent.id=@agent_id::uuid
      AND agent.project_id IS NOT DISTINCT FROM environment.project_id
      AND ((environment.scope_kind='PROJECT' AND agent.project_id=NULLIF(@project_id, '')::uuid)
        OR (environment.scope_kind='ORGANIZATION' AND agent.project_id IS NULL AND agent.system_key='system-assistant'))
    FOR SHARE OF agent SKIP LOCKED
)
SELECT binding.ref
FROM pinned_agent agent
JOIN control_plane.agent_runtime_environment_bindings binding
  ON binding.organization_id=@organization_id::uuid AND binding.agent_id=agent.id
 AND binding.environment_set_id=agent.environment_id
JOIN control_plane.runtime_environment_versions version
  ON version.organization_id=@organization_id::uuid AND version.environment_set_id=agent.environment_id
 AND version.id=CASE WHEN agent.project_id IS NULL AND agent.system_key='system-assistant'
                         AND binding.environment_version_id IS NULL
                    THEN agent.current_version_id ELSE binding.environment_version_id END
WHERE binding.id=@binding_id::uuid AND binding.ref=@binding_ref
  AND binding.version=@binding_version::bigint AND binding.digest=@binding_digest
  AND version.id=@environment_version_id::uuid AND version.digest=@environment_digest
FOR SHARE OF binding SKIP LOCKED;
