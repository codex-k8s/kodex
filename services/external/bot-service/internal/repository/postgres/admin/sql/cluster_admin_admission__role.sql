-- name: cluster_admin_admission__role :one
select exists(
	select 1
	from matter_codex_agent_roles
	where id::text = $1
		and project_id = $2
		and name = $3
		and lower(trim(kubernetes_access)) = 'cluster-admin'
)
