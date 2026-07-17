-- name: cluster_admin_bindings__chat_allowed :one
select not exists(
	select 1
	from matter_codex_agent_roles role
	where role.id = any($3::bigint[])
		and lower(trim(role.kubernetes_access)) = 'cluster-admin'
		and not exists(
			select 1
			from matter_codex_chats chat
			join matter_codex_cluster_admin_bindings binding
				on binding.chat_id = chat.id
				and binding.role_id = role.id
				and binding.project_id = role.project_id
			join matter_codex_chat_participants participant
				on participant.chat_id = chat.id
				and participant.role_id = role.id
				and participant.enabled
			where chat.project_id = $1
				and chat.slug = $2
		)
);
