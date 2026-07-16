-- name: agent_delegations__list_by_source :many
select delegations.id, delegations.project_id, delegations.source_session_id, delegations.source_turn_id,
	delegations.target_chat_id, delegations.target_role_id, delegations.target_root_post_id,
	coalesce(delegations.target_session_id, 0), coalesce(delegations.target_turn_id, 0), delegations.target_run_id,
	delegations.work_item_key, delegations.title,
	case when delegations.callback_turn_id is not null then 'callback_' || coalesce(callback_turns.status, 'queued')
		else coalesce(target_turns.status, delegations.status) end,
	coalesce(delegations.callback_turn_id, 0), delegations.callback_run_id,
	delegations.created_at, delegations.updated_at
from matter_codex_agent_delegations delegations
left join matter_codex_agent_session_turns target_turns on target_turns.id = delegations.target_turn_id
left join matter_codex_agent_session_turns callback_turns on callback_turns.id = delegations.callback_turn_id
where delegations.source_session_id = $1
order by delegations.created_at desc
limit $2;
