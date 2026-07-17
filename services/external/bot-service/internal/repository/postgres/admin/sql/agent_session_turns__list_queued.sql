select
	id,
	session_id,
	run_id,
	mattermost_channel_id,
	mattermost_root_post_id,
	mattermost_post_id,
	mattermost_status_post_id,
	mattermost_runs_post_id,
	parent_turn_ids,
	trigger_post_ids,
	initiator_user_names,
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
	updated_at
from matter_codex_agent_session_turns
where session_id = $1 and status = 'queued'
order by created_at, id;
