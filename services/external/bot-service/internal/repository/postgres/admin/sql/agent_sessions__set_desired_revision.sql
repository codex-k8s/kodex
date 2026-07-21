update matter_codex_agent_sessions
set desired_runtime_revision_id = $2,
	updated_at = now()
where session_key = $1
	and (runtime_reconcile_lease_token = '' or runtime_reconcile_lease_expires_at <= now())
returning
	id,
	session_key,
	coalesce(desired_runtime_revision_id, 0),
	coalesce(applied_runtime_revision_id, 0),
	applied_pod_uid,
	runtime_reconcile_lease_token,
	coalesce(runtime_reconcile_lease_expires_at, 'epoch'::timestamptz);
