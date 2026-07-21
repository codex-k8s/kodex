update matter_codex_agent_sessions
set applied_runtime_revision_id = case when $2 > 0 then $2 else applied_runtime_revision_id end,
	applied_pod_uid = $5,
	runtime_reconcile_lease_token = '',
	runtime_reconcile_lease_revision_id = null,
	runtime_reconcile_lease_expires_at = null,
	updated_at = now()
where session_key = $1
	and active_turn_id is null
	and coalesce(desired_runtime_revision_id, 0) = $2
	and coalesce(applied_runtime_revision_id, 0) = $3
	and applied_pod_uid = $4
	and runtime_reconcile_lease_token = $6
	and coalesce(runtime_reconcile_lease_revision_id, 0) = $2
	and runtime_reconcile_lease_expires_at > now()
	and not exists (
		select 1 from matter_codex_agent_session_turns running_turn
		where running_turn.session_id = matter_codex_agent_sessions.id and running_turn.status = 'running'
	)
returning
	id,
	session_key,
	coalesce(desired_runtime_revision_id, 0),
	coalesce(applied_runtime_revision_id, 0),
	applied_pod_uid,
	runtime_reconcile_lease_token,
	coalesce(runtime_reconcile_lease_expires_at, 'epoch'::timestamptz);
