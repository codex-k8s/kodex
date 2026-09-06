-- name: runtime_catalog__read_configuration :one
SELECT config.provider, config.model, policy.account_candidates, overlay.content
FROM control_plane.agents agent
JOIN control_plane.agent_runtime_config_versions config
  ON config.agent_id = agent.id
 AND (($3 = '' AND config.id = agent.current_runtime_config_id) OR config.id = NULLIF($3, '')::uuid)
JOIN control_plane.provider_account_policy_versions policy ON policy.id = config.provider_account_policy_id
JOIN control_plane.agent_config_overlay_versions overlay ON overlay.id = agent.current_config_overlay_id
WHERE agent.organization_id = $1::uuid AND agent.ref = $2;
