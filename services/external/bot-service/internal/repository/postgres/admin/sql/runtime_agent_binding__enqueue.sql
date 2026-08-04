-- name: RuntimeAgentBindingEnqueue :one
with owned_bot_state as (
	select
		session_row.id as agent_session_id,
		session_row.session_key as agent_session_key,
		session_row.binding_version as agent_session_version,
		turn_row.id as agent_session_turn_id,
		turn_row.run_id as agent_run_id,
		turn_row.binding_version as agent_session_turn_version
	from matter_codex_agent_session_turns turn_row
	join matter_codex_agent_sessions session_row on session_row.id = turn_row.session_id
	join matter_codex_agent_runs run_row on run_row.run_id = turn_row.run_id
	where turn_row.run_id = $12
		and turn_row.status in ('queued', 'running', 'capacity_retry', 'blocked', 'succeeded', 'failed', 'canceled')
		and session_row.status in ('starting', 'idle', 'running', 'blocked', 'closed')
	for update of session_row, turn_row
)
insert into matter_codex_runtime_agent_binding_outbox (
	idempotency_key, request_sha256,
	control_session_id, control_session_version,
	control_turn_id, control_turn_version, attempt, input_sha256,
	runtime_revision_id, runtime_revision_version, runtime_revision_sha256,
	agent_session_id, agent_session_key, agent_session_version,
	agent_session_turn_id, agent_run_id, agent_session_turn_version
)
select
	$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
	owned_bot_state.agent_session_id,
	owned_bot_state.agent_session_key,
	owned_bot_state.agent_session_version,
	owned_bot_state.agent_session_turn_id,
	owned_bot_state.agent_run_id,
	owned_bot_state.agent_session_turn_version
from owned_bot_state
on conflict (idempotency_key) do update
set idempotency_key = excluded.idempotency_key
where matter_codex_runtime_agent_binding_outbox.request_sha256 = excluded.request_sha256
returning
	id, idempotency_key, request_sha256, control_session_id,
	control_session_version, control_turn_id, control_turn_version, attempt,
	input_sha256, runtime_revision_id, runtime_revision_version,
	runtime_revision_sha256, agent_session_id, agent_session_key,
	agent_session_version, agent_session_turn_id, agent_run_id,
	agent_session_turn_version, coalesce(lease_token, '')
