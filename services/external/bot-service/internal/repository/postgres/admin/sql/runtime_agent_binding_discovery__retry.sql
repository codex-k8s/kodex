-- name: RuntimeAgentBindingDiscoveryRetry :exec
UPDATE matter_codex_runtime_agent_binding_discoveries
SET state = 'PENDING', lease_token = NULL, lease_expires_at = NULL,
	next_attempt_at = $3, last_error_code = $4
WHERE id = $1 AND state = 'LEASED' AND lease_token = $2;
