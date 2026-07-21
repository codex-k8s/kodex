update matter_codex_agent_session_turns set
	status = $5,
	final_message = $6,
	error_message = $7,
	artifacts = $8::jsonb,
	completion_pod_uid = $9,
	finished_at = now(),
	updated_at = now()
where id = $1
	and session_id = $2
	and run_id = $3
	and status = $4
	and status in ('queued', 'running')
returning
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
	updated_at,
	coalesce(runtime_revision_id, 0);
