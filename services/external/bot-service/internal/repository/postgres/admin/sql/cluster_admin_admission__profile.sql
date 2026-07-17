-- name: cluster_admin_admission__profile :one
select exists(
	select 1
	from matter_codex_agent_profiles
	where name = $1
		and name = $2
		and lower(trim(kubernetes_access)) = 'cluster-admin'
)
