-- name: agent_delegations__get_for_callback :one
select delegations.id, delegations.project_id, delegations.source_session_id, delegations.source_turn_id,
	delegations.target_chat_id, delegations.target_role_id, delegations.target_root_post_id,
	coalesce(delegations.target_session_id, 0), coalesce(delegations.target_turn_id, 0), delegations.target_run_id,
	delegations.work_item_key, delegations.title,
	case when delegations.callback_turn_id is not null then 'callback_queued'
		else coalesce(turns.status, delegations.status) end,
	coalesce(delegations.callback_turn_id, 0), delegations.callback_run_id,
	delegations.created_at, delegations.updated_at
from matter_codex_agent_delegations delegations
left join matter_codex_agent_session_turns turns on turns.id = delegations.target_turn_id
where delegations.target_session_id = $1
order by delegations.created_at desc
limit 1;
