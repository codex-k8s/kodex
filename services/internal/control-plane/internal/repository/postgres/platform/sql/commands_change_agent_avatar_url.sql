-- name: commands_change_agent_avatar_url :one
UPDATE control_plane.agents AS agent
SET avatar_url = @avatar_url,
    version = agent.version + 1,
    updated_at = clock_timestamp()
WHERE agent.organization_id = @organization_id::uuid
  AND agent.ref = @agent_ref
  AND agent.version = @expected_version
  AND agent.system_key IS NULL
RETURNING agent.project_id::text, agent.ref, agent.name, agent.purpose,
          agent.role_description, agent.avatar_url, agent.state, agent.enabled,
          agent.version, agent.created_at, agent.updated_at;
