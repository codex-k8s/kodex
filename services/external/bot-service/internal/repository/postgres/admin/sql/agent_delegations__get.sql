-- name: agent_delegations__get :one
select id, project_id, source_session_id, source_turn_id, target_chat_id, target_role_id,
	target_root_post_id, coalesce(target_session_id, 0), coalesce(target_turn_id, 0), target_run_id,
	work_item_key, title, status, coalesce(callback_turn_id, 0), callback_run_id, created_at, updated_at
from matter_codex_agent_delegations
where id = $1;
