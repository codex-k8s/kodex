select
	id,
	session_key,
	coalesce(desired_runtime_revision_id, 0),
	coalesce(applied_runtime_revision_id, 0),
	applied_pod_uid,
	runtime_reconcile_lease_token,
	coalesce(runtime_reconcile_lease_expires_at, 'epoch'::timestamptz)
from matter_codex_agent_sessions
where session_key = $1;
