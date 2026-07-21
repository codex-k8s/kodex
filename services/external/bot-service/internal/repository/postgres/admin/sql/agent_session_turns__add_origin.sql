-- name: agent_session_turns__add_origin :one
update matter_codex_agent_session_turns
set parent_turn_ids = case
		when $2::bigint <= 0 or $2::bigint = id or $2::bigint = any(parent_turn_ids) then parent_turn_ids
		else array_append(parent_turn_ids, $2::bigint)
	end,
	trigger_post_ids = case
		when btrim($3::text) = '' or $3::text = any(trigger_post_ids) then trigger_post_ids
		else array_append(trigger_post_ids, $3::text)
	end,
	initiator_user_names = case
		when btrim($4::text) = '' or $4::text = any(initiator_user_names) then initiator_user_names
		else array_append(initiator_user_names, $4::text)
	end,
	updated_at = now()
where id = $1
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
