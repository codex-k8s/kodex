-- name: agent_profiles__update_frozen_cluster_admin :one
update matter_codex_agent_profiles profile set
	role = $2,
	description = $3,
	enabled = $4,
	openai_account_name = $5,
	github_account_name = $6,
	kubernetes_access = $7,
	sandbox_mode = $8,
	config_overlay = $9,
	updated_at = now()
from matter_codex_cluster_admin_subjects frozen
where profile.name = $1
	and lower(trim($7)) = 'cluster-admin'
	and lower(trim(profile.kubernetes_access)) = 'cluster-admin'
	and frozen.subject_type = 'agent_profile'
	and frozen.subject_key = profile.name
	and frozen.project_id = 0
	and frozen.profile_name = profile.name
	and frozen.privilege_state = matter_codex_cluster_admin_profile_state(profile)
	and not exists (
		select 1 from matter_codex_cluster_admin_revocations revocation
		where revocation.resource_type = 'agent_profile'
			and revocation.resource_key = profile.name
	)
	and profile.role = $2
	and profile.description = $3
	and ($4::boolean = profile.enabled or (profile.enabled and not $4::boolean))
	and profile.openai_account_name = $5
	and profile.github_account_name = $6
	and profile.kubernetes_access = $7
	and profile.sandbox_mode = $8
	and profile.config_overlay = $9
returning
	profile.id,
	profile.name,
	profile.role,
	profile.description,
	profile.enabled,
	profile.openai_account_name,
	profile.github_account_name,
	profile.kubernetes_access,
	profile.sandbox_mode,
	profile.config_overlay,
	profile.created_at,
	profile.updated_at,
	false;
