-- name: agent_delegations__fail_by_target_turn :exec
update matter_codex_agent_delegations
set status = 'failed',
	updated_at = now()
where target_turn_id = $1
	and callback_turn_id is null
	and status <> 'failed';
