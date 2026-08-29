-- name: commands_select_agent_avatar_url :one
SELECT avatar_url
FROM control_plane.agents
WHERE organization_id = @organization_id::uuid
  AND ref = @agent_ref;
