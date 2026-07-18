-- name: cluster_admin_binding__admit :one
select exists(
	select 1
	from matter_codex_agent_roles role
	join matter_codex_cluster_admin_subjects subject
		on subject.subject_type = 'agent_role'
		and subject.subject_key = role.id::text
		and subject.project_id = role.project_id
	join matter_codex_chats chat
		on chat.project_id = role.project_id
		and (($3::bigint > 0 and chat.id = $3) or ($3::bigint = 0 and chat.slug = $4))
	join matter_codex_cluster_admin_bindings binding
		on binding.role_id = role.id
		and binding.project_id = role.project_id
		and binding.chat_id = chat.id
		and binding.mattermost_channel_id = chat.mattermost_channel_id
	join matter_codex_chat_participants participant
		on participant.role_id = role.id
		and participant.chat_id = chat.id
		and participant.enabled
	where role.id = $1
		and role.project_id = $2
		and lower(trim(role.kubernetes_access)) = 'cluster-admin'
		and role.enabled
		and matter_codex_cluster_admin_binding_exact(role.id, chat.id)
		and $5 <> ''
		and chat.mattermost_channel_id = $5
		and (
			$6 = ''
			or exists(
				select 1
				from matter_codex_cluster_admin_session_bindings frozen_session
				join matter_codex_agent_sessions session
					on session.session_key = frozen_session.session_key
					and session.role_id = frozen_session.role_id
					and session.project_id = frozen_session.project_id
					and session.chat_id = frozen_session.chat_id
					and session.mattermost_channel_id = frozen_session.mattermost_channel_id
				where frozen_session.role_id = role.id
					and frozen_session.project_id = role.project_id
					and frozen_session.chat_id = chat.id
					and frozen_session.mattermost_channel_id = chat.mattermost_channel_id
					and frozen_session.session_key = $6
					and frozen_session.privilege_state = matter_codex_cluster_admin_session_state(session)
					and not exists (
						select 1 from matter_codex_cluster_admin_revocations revocation
						where (revocation.resource_type = 'session_binding'
								and revocation.resource_key = role.id::text || ':' || frozen_session.session_key)
							or (revocation.resource_type = 'session_key'
								and revocation.resource_key = frozen_session.session_key)
					)
			)
		)
);
