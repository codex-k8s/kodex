-- name: provider_usage__agents :many
SELECT agent.ref,agent.version,agent.enabled,agent.state,COALESCE(project.ref,''),
 COALESCE(config.ref,''),COALESCE(config.digest,''),COALESCE(config.provider,''),COALESCE(config.model,''),
 COALESCE(policy.account_candidates,'[]'::jsonb),COALESCE(overlay.content,''),
 COALESCE(selected.account_ref,'')
FROM control_plane.agents agent
LEFT JOIN control_plane.projects project ON project.id=agent.project_id AND project.organization_id=agent.organization_id
LEFT JOIN control_plane.agent_runtime_config_versions config ON config.id=agent.current_runtime_config_id
LEFT JOIN control_plane.provider_account_policy_versions policy ON policy.id=config.provider_account_policy_id
LEFT JOIN control_plane.agent_config_overlay_versions overlay ON overlay.id=agent.current_config_overlay_id
LEFT JOIN LATERAL control_plane.provider_account_selection(agent.organization_id,agent.ref) selected ON true
WHERE agent.organization_id=@organization_id::uuid AND agent.ref=ANY(@agent_refs::text[])
ORDER BY agent.ref LIMIT 4097;
