-- name: agent_profiles__upsert :one
insert into matter_codex_agent_profiles(
	name,
	role,
	description,
	enabled,
	openai_account_name,
	github_account_name,
	kubernetes_access,
	sandbox_mode,
	config_overlay
) values (
	$1,
	$2,
	$3,
	$4,
	$5,
	$6,
	$7,
	$8,
	$9
)
on conflict (name) do update set
	role = excluded.role,
	description = excluded.description,
	enabled = excluded.enabled,
	openai_account_name = excluded.openai_account_name,
	github_account_name = excluded.github_account_name,
	kubernetes_access = excluded.kubernetes_access,
	sandbox_mode = excluded.sandbox_mode,
	config_overlay = excluded.config_overlay,
	updated_at = now()
returning
	id,
	name,
	role,
	description,
	enabled,
	openai_account_name,
	github_account_name,
	kubernetes_access,
	sandbox_mode,
	config_overlay,
	created_at,
	updated_at,
	(xmax = 0) as created;
