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
from matter_codex_cluster_admin_subjects frozen,
	matter_codex_projects project
where role.project_id = $1
	and role.name = $2
	and lower(trim($9)) = 'cluster-admin'
	and lower(trim(role.kubernetes_access)) = 'cluster-admin'
	and frozen.subject_type = 'agent_role'
	and frozen.subject_key = role.id::text
	and frozen.project_id = role.project_id
	and frozen.profile_name = role.name
	and project.id = role.project_id
	and frozen.privilege_state = matter_codex_cluster_admin_role_state(role)
	and not exists (
		select 1 from matter_codex_cluster_admin_revocations revocation
		where revocation.resource_type = 'agent_role'
			and revocation.resource_key = role.id::text
	)
	and (
		frozen.privilege_state = jsonb_build_object(
			'project_id', $1::bigint,
			'name', $2::text,
			'role_type', $3::text,
			'description', $4::text,
			'prompt_template', nullif($5::text, ''),
			'prompt_mode', $6::text,
			'github_account_name', $7::text,
			'project_github_account_name', project.github_account_name,
			'project_slug', project.slug,
			'project_mattermost_team_id', project.mattermost_team_id,
			'project_github_owner', project.github_owner,
			'project_github_owner_type', project.github_owner_type,
			'openai_account_name', $8::text,
			'kubernetes_access', $9::text,
			'sandbox_mode', $10::text,
			'config_overlay', $11::text,
			'advanced_settings', coalesce(nullif($12::text, '')::jsonb, '{}'::jsonb),
			'enabled', $13::boolean,
			'bot_identity', $14::text
		)
		or (
			role.enabled
			and not $13::boolean
			and (frozen.privilege_state - 'enabled') = (
				jsonb_build_object(
					'project_id', $1::bigint,
					'name', $2::text,
					'role_type', $3::text,
					'description', $4::text,
					'prompt_template', nullif($5::text, ''),
					'prompt_mode', $6::text,
					'github_account_name', $7::text,
					'project_github_account_name', project.github_account_name,
					'project_slug', project.slug,
					'project_mattermost_team_id', project.mattermost_team_id,
					'project_github_owner', project.github_owner,
					'project_github_owner_type', project.github_owner_type,
					'openai_account_name', $8::text,
					'kubernetes_access', $9::text,
					'sandbox_mode', $10::text,
					'config_overlay', $11::text,
					'advanced_settings', coalesce(nullif($12::text, '')::jsonb, '{}'::jsonb),
					'enabled', $13::boolean,
					'bot_identity', $14::text
				) - 'enabled'
			)
		)
	)
returning role.id, role.project_id, role.name, role.role_type, role.description,
	coalesce(role.prompt_template, ''), role.prompt_mode, role.github_account_name,
	role.openai_account_name, role.kubernetes_access, role.sandbox_mode,
	role.config_overlay, role.advanced_settings::text, role.enabled, role.bot_identity,
	role.created_at, role.updated_at, false;
