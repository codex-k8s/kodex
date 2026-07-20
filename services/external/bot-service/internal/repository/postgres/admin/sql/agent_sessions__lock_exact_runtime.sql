select
	session_row.id,
	session_row.session_key,
	session_row.project_id,
	session_row.chat_id,
	session_row.role_id,
	session_row.session_scope,
	session_row.mattermost_channel_id,
	session_row.mattermost_root_post_id,
	session_row.codex_session_id,
	session_row.status,
	coalesce(session_row.active_turn_id, 0),
	session_row.active_run_id,
	session_row.kubernetes_namespace,
	session_row.pod_name,
	session_row.pvc_name,
	session_row.token_secret_ref,
	session_row.capabilities::text,
	session_row.session_archive_gzip_base64,
	session_row.ttl_seconds,
	session_row.last_activity_at,
	session_row.expires_at,
	session_row.created_at,
	session_row.updated_at,
	session_row.openai_account_name,
	role_row.project_id,
	role_row.enabled,
	chat_row.project_id,
	chat_row.mattermost_channel_id,
	participant.enabled
from matter_codex_agent_sessions session_row
join matter_codex_agent_roles role_row on role_row.id = session_row.role_id
join matter_codex_chats chat_row on chat_row.id = session_row.chat_id
join matter_codex_chat_participants participant
	on participant.chat_id = session_row.chat_id
	and participant.role_id = session_row.role_id
where session_row.session_key = $1
for share of session_row;
