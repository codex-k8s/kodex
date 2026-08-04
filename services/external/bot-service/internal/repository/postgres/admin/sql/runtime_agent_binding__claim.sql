-- name: RuntimeAgentBindingClaim :one
with candidate as (
	select id
	from matter_codex_runtime_agent_binding_outbox
	where (
		state = 'PENDING'
		or (state = 'LEASED' and lease_expires_at <= transaction_timestamp())
	)
		and next_attempt_at <= transaction_timestamp()
	order by next_attempt_at, id
	for update skip locked
	limit 1
)
update matter_codex_runtime_agent_binding_outbox delivery
set state = 'LEASED',
	lease_token = $1,
	lease_expires_at = $2,
	delivery_attempt = delivery_attempt + 1
from candidate
where delivery.id = candidate.id
returning
	delivery.id, delivery.idempotency_key, delivery.request_sha256,
	delivery.control_session_id, delivery.control_session_version,
	delivery.control_turn_id, delivery.control_turn_version, delivery.attempt,
	delivery.input_sha256, delivery.runtime_revision_id,
	delivery.runtime_revision_version, delivery.runtime_revision_sha256,
	delivery.agent_session_id, delivery.agent_session_key,
	delivery.agent_session_version, delivery.agent_session_turn_id,
	delivery.agent_run_id, delivery.agent_session_turn_version,
	delivery.lease_token
