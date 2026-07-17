select
	sessions.id,
	sessions.session_key,
	sessions.project_id,
	sessions.chat_id,
	sessions.role_id,
	sessions.session_scope,
	sessions.mattermost_channel_id,
	sessions.mattermost_root_post_id,
	sessions.codex_session_id,
	sessions.status,
	coalesce(sessions.active_turn_id, 0),
	sessions.active_run_id,
	sessions.kubernetes_namespace,
	sessions.pod_name,
	sessions.pvc_name,
	sessions.token_secret_ref,
	sessions.capabilities::text,
	sessions.session_archive_gzip_base64,
	sessions.ttl_seconds,
	sessions.last_activity_at,
	sessions.expires_at,
	sessions.created_at,
	sessions.updated_at,
	sessions.openai_account_name
from matter_codex_agent_sessions sessions
where sessions.status = 'idle'
	and sessions.active_turn_id is null
	and sessions.pod_name <> ''
	and not exists (
		select 1
		from matter_codex_agent_session_turns turns
		where turns.session_id = sessions.id
			and turns.status in ('queued', 'running')
	)
order by sessions.last_activity_at, sessions.id
limit $1;
