-- name: agent_delegations__insert :one
insert into matter_codex_agent_delegations(
	project_id,
	source_session_id,
	source_turn_id,
	target_chat_id,
	target_role_id,
	work_item_key,
	title
) values ($1, $2, $3, $4, $5, $6, $7)
on conflict (source_session_id, work_item_key) do nothing
returning id, project_id, source_session_id, source_turn_id, target_chat_id, target_role_id,
	target_root_post_id, coalesce(target_session_id, 0), coalesce(target_turn_id, 0), target_run_id,
	work_item_key, title, status, coalesce(callback_turn_id, 0), callback_run_id, created_at, updated_at;
