update matter_codex_scheduled_runs
set mattermost_channel_id = $2,
	mattermost_root_post_id = $3,
	updated_at = $4
where id = $1
	and status = 'queued';
