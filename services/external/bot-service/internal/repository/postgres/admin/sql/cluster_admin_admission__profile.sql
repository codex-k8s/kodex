-- name: cluster_admin_admission__profile :one
select exists(
	select 1
	from matter_codex_agent_profiles profile
	join matter_codex_cluster_admin_subjects frozen
		on frozen.subject_type = 'agent_profile'
		and frozen.subject_key = profile.name
		and frozen.project_id = 0
	where profile.name = $1
		and profile.name = $2
		and frozen.profile_name = profile.name
		and lower(trim(profile.kubernetes_access)) = 'cluster-admin'
		and profile.enabled
		and matter_codex_cluster_admin_profile_exact(profile.name)
)
