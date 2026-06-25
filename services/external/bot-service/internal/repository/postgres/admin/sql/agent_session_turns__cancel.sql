update matter_codex_agent_session_turns set
	status = 'canceled',
	error_message = $2,
	artifacts = $3::jsonb,
	finished_at = now(),
	updated_at = now()
where id = $1 and status in ('queued', 'running')
returning
	id,
	session_id,
	run_id,
	mattermost_channel_id,
	mattermost_root_post_id,
	mattermost_post_id,
	mattermost_status_post_id,
	user_id,
	user_name,
	message,
	status,
	final_message,
	error_message,
	artifacts::text,
	created_at,
	coalesce(started_at, 'epoch'::timestamptz),
	coalesce(finished_at, 'epoch'::timestamptz),
	updated_at;
