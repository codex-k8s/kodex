update matter_codex_agent_sessions set
	status = case
		when $2 = 'idle' and active_turn_id is not null then status
		when $2 <> '' then $2
		else status
	end,
	active_turn_id = case when $3 > 0 then $3 else active_turn_id end,
	active_run_id = case when $4 <> '' then $4 else active_run_id end,
	mattermost_root_post_id = case when $5 <> '' then $5 else mattermost_root_post_id end,
	kubernetes_namespace = case when $6 <> '' then $6 else kubernetes_namespace end,
	pod_name = case when $7 <> '' then $7 else pod_name end,
	pvc_name = case when $8 <> '' then $8 else pvc_name end,
	token_secret_ref = case when $9 <> '' then $9 else token_secret_ref end,
	desired_runtime_revision_id = case when $11 > 0 then $11 else desired_runtime_revision_id end,
	applied_runtime_revision_id = case when $12 > 0 then $12 else applied_runtime_revision_id end,
	last_activity_at = now(),
	expires_at = case when $10 > 0 then now() + make_interval(secs => $10::int) else expires_at end,
	updated_at = now()
where session_key = $1
returning
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
	updated_at,
	openai_account_name;
