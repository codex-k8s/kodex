-- name: cluster_admin_session__requires_guard :one
select exists (
	select 1
	from matter_codex_cluster_admin_session_bindings
	where role_id = $1
		and session_key = $2
)
or exists (
	select 1
	from matter_codex_agent_roles
	where id = $1
		and lower(trim(kubernetes_access)) = 'cluster-admin'
);
