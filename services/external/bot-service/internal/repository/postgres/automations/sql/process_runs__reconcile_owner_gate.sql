-- name: process_runs__reconcile_owner_gate :exec
with process_state as (
	select
		process.id,
		exists (
			select 1
			from matter_codex_owner_attention_requests attention
			where attention.process_run_id = process.id
				and attention.status = 'open'
		) as has_open_attention,
		exists (
			select 1
			from matter_codex_process_turns process_turn
			join matter_codex_agent_session_turns turn
				on turn.id = process_turn.turn_id
			where process_turn.process_run_id = process.id
				and turn.status in ('queued', 'running', 'capacity_retry')
		) as has_active_turn
	from matter_codex_process_runs process
	where process.id = $1
)
update matter_codex_process_runs process
set status = case
		when state.has_open_attention then 'waiting_owner'
		when state.has_active_turn then 'running'
		else 'completed'
	end,
	finished_at = case
		when state.has_open_attention or state.has_active_turn then null
		else coalesce(process.finished_at, $2)
	end,
	updated_at = $2
from process_state state
where process.id = state.id;
