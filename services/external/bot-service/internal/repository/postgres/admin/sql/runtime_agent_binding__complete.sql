-- name: RuntimeAgentBindingComplete :exec
update matter_codex_runtime_agent_binding_outbox
set state = 'DELIVERED',
	lease_token = null,
	lease_expires_at = null,
	agent_session_binding_sha256 = $3,
	agent_turn_binding_sha256 = $4,
	delivered_at = transaction_timestamp(),
	last_error_code = null
where id = $1
	and state = 'LEASED'
	and lease_token = $2
