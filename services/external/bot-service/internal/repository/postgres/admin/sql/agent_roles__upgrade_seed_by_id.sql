-- name: agent_roles__upgrade_seed_by_id :one
update matter_codex_agent_roles
set prompt_template = $3,
	updated_at = now()
where id = $1
	and prompt_template = $2
	and lower(btrim(kubernetes_access)) <> 'cluster-admin'
returning
	id, project_id, name, role_type, description, coalesce(prompt_template, ''),
	prompt_mode, github_account_name, openai_account_name, kubernetes_access,
	sandbox_mode, config_overlay, advanced_settings::text, enabled, bot_identity,
	created_at, updated_at;
