-- name: cluster_admin_admission__role :one
select exists(
	select 1
	from matter_codex_agent_roles role
	join matter_codex_cluster_admin_subjects frozen
		on frozen.subject_type = 'agent_role'
		and frozen.subject_key = role.id::text
		and frozen.project_id = role.project_id
	where role.id::text = $1
		and role.project_id = $2
		and role.name = $3
		and frozen.profile_name = role.name
		and lower(trim(role.kubernetes_access)) = 'cluster-admin'
)
