update matter_codex_agent_sessions set
	runtime_reconcile_lease_token = '',
	runtime_reconcile_lease_revision_id = null,
	runtime_reconcile_lease_expires_at = null,
	updated_at = now()
where session_key = $1 and runtime_reconcile_lease_token = $2;
-- name: agent_sessions__release_runtime_lease :exec
