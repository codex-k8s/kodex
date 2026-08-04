-- name: RuntimeAgentBindingRetry :exec
update matter_codex_runtime_agent_binding_outbox
set state = 'PENDING',
	lease_token = null,
	lease_expires_at = null,
	next_attempt_at = $3,
	last_error_code = $4
where id = $1
	and state = 'LEASED'
	and lease_token = $2
