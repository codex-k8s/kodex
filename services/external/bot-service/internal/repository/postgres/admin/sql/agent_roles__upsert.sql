-- name: agent_roles__upsert :one
insert into matter_codex_agent_roles(
	project_id,
	name,
	role_type,
	description,
	prompt_template,
	prompt_mode,
	github_account_name,
	openai_account_name,
	kubernetes_access,
	sandbox_mode,
	config_overlay,
	advanced_settings,
	enabled,
	bot_identity
) select
	$1,
	$2,
	$3,
	$4,
	nullif($5, ''),
	$6,
	$7,
	$8,
	$9,
	$10,
	$11,
	coalesce(nullif($12, '')::jsonb, '{}'::jsonb),
	$13,
	$14
where lower(trim($9)) <> 'cluster-admin'
on conflict (project_id, name) do update set
	role_type = excluded.role_type,
	description = excluded.description,
	prompt_template = excluded.prompt_template,
	prompt_mode = excluded.prompt_mode,
	github_account_name = excluded.github_account_name,
	openai_account_name = excluded.openai_account_name,
	kubernetes_access = excluded.kubernetes_access,
	sandbox_mode = excluded.sandbox_mode,
	config_overlay = excluded.config_overlay,
	advanced_settings = excluded.advanced_settings,
	enabled = excluded.enabled,
	bot_identity = excluded.bot_identity,
	updated_at = now()
where lower(trim(excluded.kubernetes_access)) <> 'cluster-admin'
returning id, project_id, name, role_type, description, coalesce(prompt_template, ''), prompt_mode, github_account_name, openai_account_name, kubernetes_access, sandbox_mode, config_overlay, advanced_settings::text, enabled, bot_identity, created_at, updated_at, (xmax = 0) as created;
