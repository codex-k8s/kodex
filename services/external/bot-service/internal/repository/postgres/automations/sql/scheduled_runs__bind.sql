update matter_codex_scheduled_runs
set status = 'running',
	runtime_session_id = $2,
	runtime_session_key = $3,
	runtime_turn_id = $4,
	runtime_run_id = $5,
	mattermost_channel_id = $6,
	mattermost_root_post_id = $7,
	started_at = $8,
	updated_at = $8
where id = $1;
