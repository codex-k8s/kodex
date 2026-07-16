-- name: agent_delegations__set_root :one
update matter_codex_agent_delegations set
	target_root_post_id = $2,
	status = 'thread_created',
	updated_at = now()
where id = $1
returning id, project_id, source_session_id, source_turn_id, target_chat_id, target_role_id,
	target_root_post_id, coalesce(target_session_id, 0), coalesce(target_turn_id, 0), target_run_id,
	work_item_key, title, status, coalesce(callback_turn_id, 0), callback_run_id, created_at, updated_at;
