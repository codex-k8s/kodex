-- name: RuntimeAgentBindingDiscoveryClaim :one
WITH candidate AS (
	SELECT id
	FROM matter_codex_runtime_agent_binding_discoveries
	WHERE (state = 'PENDING' OR (state = 'LEASED' AND lease_expires_at <= transaction_timestamp()))
	  AND next_attempt_at <= transaction_timestamp()
	ORDER BY next_attempt_at, id
	FOR UPDATE SKIP LOCKED
	LIMIT 1
)
,
leased AS (
	UPDATE matter_codex_runtime_agent_binding_discoveries AS discovery
	SET state = 'LEASED', lease_token = $1, lease_expires_at = $2,
		attempt_count = attempt_count + 1
	FROM candidate
	WHERE discovery.id = candidate.id
	RETURNING discovery.*
)
SELECT leased.id, leased.agent_session_id, leased.agent_session_turn_id,
	leased.agent_session_version, leased.agent_session_turn_version,
	leased.agent_run_id, leased.source_ref, leased.lease_token,
	leased.agent_session_key, leased.role_stable_key,
	leased.external_channel_ref, leased.prompt_text
FROM leased;
