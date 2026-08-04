-- name: RuntimeAgentBindingDiscoveryComplete :exec
UPDATE matter_codex_runtime_agent_binding_discoveries
SET state = 'DELIVERED', lease_token = NULL, lease_expires_at = NULL,
	delivered_at = transaction_timestamp(), last_error_code = NULL
WHERE id = $1 AND state = 'LEASED' AND lease_token = $2;
