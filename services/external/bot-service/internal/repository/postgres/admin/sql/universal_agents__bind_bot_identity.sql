-- name: universal_agents__bind_bot_identity :exec
update matter_codex_agents
set bot_identity_id = $2,
	record_version = record_version + 1,
	updated_at = now()
where legacy_agent_role_id = $1;
