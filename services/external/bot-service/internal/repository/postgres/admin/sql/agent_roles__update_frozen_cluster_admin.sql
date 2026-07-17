-- name: agent_roles__update_frozen_cluster_admin :one
update matter_codex_agent_roles role set
	role_type = $3,
	description = $4,
	prompt_template = nullif($5, ''),
	prompt_mode = $6,
	github_account_name = $7,
	openai_account_name = $8,
	kubernetes_access = $9,
	sandbox_mode = $10,
	config_overlay = $11,
	advanced_settings = coalesce(nullif($12, '')::jsonb, '{}'::jsonb),
	enabled = $13,
	bot_identity = $14,
	updated_at = now()
from matter_codex_cluster_admin_subjects frozen
where role.project_id = $1
	and role.name = $2
	and lower(trim($9)) = 'cluster-admin'
	and lower(trim(role.kubernetes_access)) = 'cluster-admin'
	and frozen.subject_type = 'agent_role'
	and frozen.subject_key = role.id::text
	and frozen.project_id = role.project_id
	and frozen.profile_name = role.name
returning role.id, role.project_id, role.name, role.role_type, role.description,
	coalesce(role.prompt_template, ''), role.prompt_mode, role.github_account_name,
	role.openai_account_name, role.kubernetes_access, role.sandbox_mode,
	role.config_overlay, role.advanced_settings::text, role.enabled, role.bot_identity,
	role.created_at, role.updated_at, false;
