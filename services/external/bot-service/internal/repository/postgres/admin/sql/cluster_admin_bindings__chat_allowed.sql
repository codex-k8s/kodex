-- name: cluster_admin_bindings__chat_allowed :one
select not exists(
	select 1
	from matter_codex_agent_roles role
	where role.id = any($4::bigint[])
		and lower(trim(role.kubernetes_access)) = 'cluster-admin'
		and role.enabled
		and not exists (
			select 1 from matter_codex_chats chat
			where chat.project_id = $1
				and chat.slug = $2
				and chat.mattermost_channel_id = $3
				and matter_codex_cluster_admin_binding_exact(role.id, chat.id)
		)
);
