insert into matter_codex_agent_session_turns (
	session_id,
	run_id,
	mattermost_channel_id,
	mattermost_root_post_id,
	mattermost_post_id,
	user_id,
	user_name,
	message
) values ($1, $2, $3, $4, $5, $6, $7, $8)
returning
	id,
	session_id,
	run_id,
	mattermost_channel_id,
	mattermost_root_post_id,
	mattermost_post_id,
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
