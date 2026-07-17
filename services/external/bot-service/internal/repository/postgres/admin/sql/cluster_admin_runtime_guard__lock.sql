-- name: cluster_admin_runtime_guard__lock :one
select true
from matter_codex_agent_roles role
join matter_codex_projects project on project.id = role.project_id
join matter_codex_cluster_admin_subjects subject
	on subject.subject_type = 'agent_role'
	and subject.subject_key = role.id::text
	and subject.project_id = role.project_id
join matter_codex_mattermost_bot_identities bot on bot.role_id = role.id
join matter_codex_cluster_admin_bot_bindings frozen_bot
	on frozen_bot.role_id = role.id
	and frozen_bot.project_id = role.project_id
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
	and role.enabled
	and lower(trim(role.kubernetes_access)) = 'cluster-admin'
	and matter_codex_cluster_admin_binding_exact(role.id, chat.id)
	and $5 <> ''
	and chat.mattermost_channel_id = $5
for share of project, role, subject, bot, frozen_bot, chat, binding, participant
