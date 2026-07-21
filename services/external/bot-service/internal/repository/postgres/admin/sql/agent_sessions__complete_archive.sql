update matter_codex_agent_sessions
set
	codex_session_id = case when $2 <> '' then $2 else codex_session_id end,
	session_archive_gzip_base64 = case when $3 <> '' then $3 else session_archive_gzip_base64 end,
	archive_version = case when $3 <> '' then $4 else archive_version end,
	archive_sha256 = case when $3 <> '' then $5 else archive_sha256 end,
	archive_size_bytes = case when $3 <> '' then $6 else archive_size_bytes end,
	status = case when $7 <> '' then $7 else status end,
	active_turn_id = null,
	active_run_id = '',
	desired_runtime_revision_id = coalesce(
		(
			select queued.runtime_revision_id
			from matter_codex_agent_session_turns queued
			where queued.session_id = matter_codex_agent_sessions.id
				and queued.status = 'queued'
				and queued.runtime_revision_id is not null
			order by queued.created_at, queued.id
			limit 1
		),
		applied_runtime_revision_id,
		desired_runtime_revision_id
	),
	last_activity_at = now(),
	expires_at = case when $8 > 0 then now() + make_interval(secs => $8::int) else expires_at end,
	updated_at = now()
where session_key = $1;
