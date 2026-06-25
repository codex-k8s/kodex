select
	id,
	session_key,
	project_id,
	chat_id,
	role_id,
	session_scope,
	mattermost_channel_id,
	mattermost_root_post_id,
	codex_session_id,
	status,
	coalesce(active_turn_id, 0),
	active_run_id,
	kubernetes_namespace,
	pod_name,
	pvc_name,
	token_secret_ref,
	capabilities::text,
	session_archive_gzip_base64,
	ttl_seconds,
	last_activity_at,
	expires_at,
	created_at,
	updated_at
from matter_codex_agent_sessions
where role_id = $1
order by updated_at desc;
