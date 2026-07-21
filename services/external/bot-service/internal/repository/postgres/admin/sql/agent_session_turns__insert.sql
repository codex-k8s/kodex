insert into matter_codex_agent_session_turns (
	session_id,
	run_id,
	mattermost_channel_id,
	mattermost_root_post_id,
	mattermost_post_id,
	parent_turn_ids,
	trigger_post_ids,
	initiator_user_names,
	user_id,
	user_name,
	message
) values (
	$1,
	$2,
	$3,
	$4,
	$5,
	case when $6::bigint > 0 then array[$6::bigint] else '{}'::bigint[] end,
	case when btrim($5::text) <> '' then array[$5::text] else '{}'::text[] end,
	case when btrim($8::text) <> '' then array[$8::text] else '{}'::text[] end,
	$7,
	$8,
	$9
)
on conflict (run_id) do update
set run_id = excluded.run_id
where matter_codex_agent_session_turns.session_id = excluded.session_id
	and matter_codex_agent_session_turns.mattermost_channel_id = excluded.mattermost_channel_id
	and matter_codex_agent_session_turns.mattermost_root_post_id = excluded.mattermost_root_post_id
	and matter_codex_agent_session_turns.mattermost_post_id = excluded.mattermost_post_id
	and matter_codex_agent_session_turns.user_id = excluded.user_id
	and matter_codex_agent_session_turns.user_name = excluded.user_name
	and matter_codex_agent_session_turns.message = excluded.message
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
	updated_at;
