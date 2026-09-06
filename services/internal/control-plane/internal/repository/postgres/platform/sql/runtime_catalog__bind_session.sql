-- name: runtime_catalog__bind_session :exec
INSERT INTO control_plane.session_model_catalog_bindings
  (session_id, organization_id, provider_account_id, provider_account_policy_id, catalog_revision, catalog_digest, models)
SELECT session.id, session.organization_id, account.id, policy.id, $4, $5, $6::jsonb
FROM control_plane.sessions session
JOIN control_plane.provider_accounts account ON account.id = session.provider_account_id AND account.organization_id = session.organization_id
JOIN control_plane.agents agent ON agent.organization_id = session.organization_id AND agent.ref = $3
JOIN control_plane.agent_runtime_config_versions config ON config.id = agent.current_runtime_config_id
JOIN control_plane.provider_account_policy_versions policy ON policy.id = config.provider_account_policy_id AND policy.organization_id = session.organization_id
WHERE session.id = $1::uuid AND session.organization_id = $2::uuid;
